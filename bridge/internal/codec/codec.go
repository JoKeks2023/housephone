// Package codec lists the audio codecs the bridge passes through end to end
// (FRITZ!Box ⇄ bridge ⇄ device) without transcoding.
package codec

import (
	"fmt"
	"strings"
)

// Codec is an RTP audio codec by its SDP encoding name.
type Codec string

const (
	G722 Codec = "G722"
	PCMA Codec = "PCMA"
	PCMU Codec = "PCMU"
)

// Preferred is the negotiation order: HD voice first.
var Preferred = []Codec{G722, PCMA, PCMU}

// ClockRate is the RTP clock rate. G.722 uses 8000 for historic reasons
// (RFC 3551 §4.5.2) although it samples at 16 kHz.
const ClockRate = 8000

// SamplesPerPacket is the RTP timestamp increment for 20 ms packets.
const SamplesPerPacket = ClockRate / 50

// PayloadType returns the static RTP payload type (RFC 3551).
func (c Codec) PayloadType() uint8 {
	switch c {
	case G722:
		return 9
	case PCMA:
		return 8
	case PCMU:
		return 0
	}
	panic(fmt.Sprintf("unknown codec %q", string(c)))
}

// MimeType returns the WebRTC MIME type, e.g. "audio/G722".
func (c Codec) MimeType() string {
	return "audio/" + string(c)
}

// Valid reports whether c is one of the supported codecs.
func (c Codec) Valid() bool {
	switch c {
	case G722, PCMA, PCMU:
		return true
	}
	return false
}

// FromName parses an SDP encoding name case-insensitively.
func FromName(name string) (Codec, bool) {
	c := Codec(strings.ToUpper(name))
	return c, c.Valid()
}

// FromMimeType parses a WebRTC MIME type such as "audio/g722".
func FromMimeType(mime string) (Codec, bool) {
	kind, name, ok := strings.Cut(mime, "/")
	if !ok || !strings.EqualFold(kind, "audio") {
		return "", false
	}
	return FromName(name)
}

// ChooseFirst returns the first preferred codec contained in offered.
func ChooseFirst(offered []Codec) (Codec, bool) {
	for _, want := range Preferred {
		for _, c := range offered {
			if c == want {
				return want, true
			}
		}
	}
	return "", false
}
