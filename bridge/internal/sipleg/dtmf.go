package sipleg

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/pion/rtp"
)

// RFC 4733 timing for generated key presses.
const (
	dtmfToneDuration  = 160 * time.Millisecond
	dtmfPacketEvery   = 50 * time.Millisecond
	dtmfGap           = 100 * time.Millisecond
	dtmfEndRepeats    = 3
	dtmfVolume        = 10 // -10 dBm0
	dtmfUnitsPerMilli = 8  // 8 kHz clock
)

// dtmfEvent maps a key to its RFC 4733 event code.
func dtmfEvent(key rune) (uint8, error) {
	switch {
	case key >= '0' && key <= '9':
		return uint8(key - '0'), nil
	case key == '*':
		return 10, nil
	case key == '#':
		return 11, nil
	}
	return 0, fmt.Errorf("unsupported DTMF key %q", key)
}

// dtmfPayload encodes one telephone-event payload.
func dtmfPayload(event uint8, end bool, duration uint16) []byte {
	b := make([]byte, 4)
	b[0] = event
	b[1] = dtmfVolume & 0x3f
	if end {
		b[1] |= 0x80
	}
	binary.BigEndian.PutUint16(b[2:], duration)
	return b
}

// dtmfPackets returns the packets of one key press, all sharing the event
// timestamp: progress updates every 50 ms and three end packets. The caller
// sets SSRC and sequence numbers.
func dtmfPackets(event uint8, payloadType uint8, timestamp uint32) []*rtp.Packet {
	var pkts []*rtp.Packet
	step := uint16(dtmfPacketEvery.Milliseconds() * dtmfUnitsPerMilli)
	total := uint16(dtmfToneDuration.Milliseconds() * dtmfUnitsPerMilli)
	for d := step; d < total; d += step {
		pkts = append(pkts, &rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: payloadType, Timestamp: timestamp, Marker: len(pkts) == 0},
			Payload: dtmfPayload(event, false, d),
		})
	}
	for range dtmfEndRepeats {
		pkts = append(pkts, &rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: payloadType, Timestamp: timestamp},
			Payload: dtmfPayload(event, true, total),
		})
	}
	return pkts
}
