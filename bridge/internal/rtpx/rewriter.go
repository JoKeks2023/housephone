// Package rtpx rewrites RTP headers when forwarding a stream between the
// FRITZ!Box leg and a device leg.
package rtpx

import (
	"github.com/pion/rtp"
)

// Rewriter keeps an outgoing RTP stream continuous while the incoming stream
// may change SSRC (e.g. after a re-INVITE or ICE restart). Sequence numbers
// and timestamps keep their gaps (loss and silence stay visible to the
// receiver) but jump seamlessly on SSRC changes.
//
// A Rewriter is not safe for concurrent use; use one per direction.
type Rewriter struct {
	// SSRC, when non-zero, replaces the packet SSRC.
	SSRC uint32
	// PayloadType replaces the payload type when SetPayloadType is true.
	PayloadType    uint8
	SetPayloadType bool
	// TimestampStep is the expected timestamp increment per packet, used to
	// continue the timeline after an SSRC change.
	TimestampStep uint32
	// StripExtensions removes header extensions and CSRCs, which are
	// meaningless (or unknown) on the other leg.
	StripExtensions bool

	started    bool
	inSSRC     uint32
	seqOffset  uint16
	tsOffset   uint32
	lastOutSeq uint16
	lastOutTS  uint32
}

// Rewrite modifies p in place.
func (r *Rewriter) Rewrite(p *rtp.Packet) {
	switch {
	case !r.started:
		r.inSSRC = p.SSRC
	case p.SSRC != r.inSSRC:
		// New incoming stream: continue right after the last output packet.
		r.inSSRC = p.SSRC
		r.seqOffset = r.lastOutSeq + 1 - p.SequenceNumber
		r.tsOffset = r.lastOutTS + r.TimestampStep - p.Timestamp
		p.Marker = true
	}

	p.SequenceNumber += r.seqOffset
	p.Timestamp += r.tsOffset
	if !r.started || seqNewer(p.SequenceNumber, r.lastOutSeq) {
		r.lastOutSeq = p.SequenceNumber
		r.lastOutTS = p.Timestamp
	}
	r.started = true

	if r.SSRC != 0 {
		p.SSRC = r.SSRC
	}
	if r.SetPayloadType {
		p.PayloadType = r.PayloadType
	}
	if r.StripExtensions {
		p.Extension = false
		p.ExtensionProfile = 0
		p.Extensions = nil
		p.CSRC = nil
	}
}

// seqNewer reports whether a is after b in RTP sequence space (RFC 3550 A.1).
func seqNewer(a, b uint16) bool {
	return a != b && uint16(a-b) < 0x8000
}
