package calls

import (
	"crypto/rand"
	"encoding/binary"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/rtpx"
)

// relay forwards RTP payloads between the SIP leg and one device peer
// without transcoding. Both legs use the same codec.
type relay struct {
	sip     SIPMedia
	peer    Peer
	stopped atomic.Bool
	wg      sync.WaitGroup

	// Counters for logs and tests.
	toDevice atomic.Uint64
	toSIP    atomic.Uint64
}

func startRelay(sip SIPMedia, peer Peer, c codec.Codec, log *slog.Logger) *relay {
	r := &relay{sip: sip, peer: peer}
	pt := c.PayloadType()
	log = log.With("codec", c)
	log.Info("media relay started")

	r.wg.Add(2)
	go func() {
		defer r.wg.Done()
		rw := rtpx.Rewriter{TimestampStep: codec.SamplesPerPacket, StripExtensions: true}
		buf := make([]byte, 1500)
		var pkt rtp.Packet
		for {
			n, err := sip.ReadRTP(buf, &pkt)
			if err != nil || r.stopped.Load() {
				log.Debug("SIP→device relay stopped", "error", err, "packets", r.toDevice.Load())
				return
			}
			if n == 0 || pkt.PayloadType != pt {
				// DTMF (telephone-event), comfort noise or keepalives.
				continue
			}
			rw.Rewrite(&pkt)
			if err := peer.WriteRTP(&pkt); err != nil {
				if r.stopped.Load() {
					return
				}
				log.Debug("writing to device failed", "error", err)
				continue
			}
			r.toDevice.Add(1)
		}
	}()
	go func() {
		defer r.wg.Done()
		rw := rtpx.Rewriter{
			SSRC:            randomSSRC(),
			PayloadType:     pt,
			SetPayloadType:  true,
			TimestampStep:   codec.SamplesPerPacket,
			StripExtensions: true,
		}
		for {
			pkt, err := peer.ReadRTP()
			if err != nil || r.stopped.Load() {
				log.Debug("device→SIP relay stopped", "error", err, "packets", r.toSIP.Load())
				return
			}
			rw.Rewrite(pkt)
			if err := sip.WriteRTP(pkt); err != nil {
				if r.stopped.Load() {
					return
				}
				log.Debug("writing to SIP failed", "error", err)
				continue
			}
			r.toSIP.Add(1)
		}
	}()
	return r
}

// stop marks the relay stopped. Goroutines exit on their next packet or when
// the underlying media or peer closes.
func (r *relay) stop() {
	r.stopped.Store(true)
}

func randomSSRC() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	if v := binary.BigEndian.Uint32(b[:]); v != 0 {
		return v
	}
	return 1
}
