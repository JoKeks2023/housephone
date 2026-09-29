package sipleg

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
)

var errMediaClosed = errors.New("SIP media closed")

// sipMedia adapts a diago dialog's RTP session to calls.SIPMedia.
//
// Outgoing packets all get the same SSRC. DTMF events are injected into that
// same stream (RFC 4733 requires one SSRC); their sequence numbers are
// borrowed by shifting the audio sequence offset, so gaps from the device
// stay visible to the FRITZ!Box.
type sipMedia struct {
	med *diago.DialogMedia

	// writeMu serializes writes: diago's WriteRTP shares one buffer.
	writeMu sync.Mutex

	mu         sync.Mutex
	seqOffset  uint16
	lastSeq    uint16
	lastTS     uint32
	started    bool
	ssrc       uint32
	dtmfActive bool
}

func newSIPMedia(med *diago.DialogMedia) *sipMedia {
	return &sipMedia{med: med}
}

func (m *sipMedia) session() (*media.RTPSession, error) {
	s := m.med.RTPSession()
	if s == nil {
		return nil, errMediaClosed
	}
	return s, nil
}

// ReadRTP implements calls.SIPMedia. A re-INVITE may replace the RTP
// session; reads then continue on the new one.
func (m *sipMedia) ReadRTP(buf []byte, pkt *rtp.Packet) (int, error) {
	for {
		s, err := m.session()
		if err != nil {
			return 0, err
		}
		n, err := s.ReadRTP(buf, pkt)
		if err != nil {
			if next := m.med.RTPSession(); next != nil && next != s {
				continue
			}
			return n, err
		}
		return n, nil
	}
}

// WriteRTP implements calls.SIPMedia. Audio is dropped while a DTMF event is
// being sent.
func (m *sipMedia) WriteRTP(pkt *rtp.Packet) error {
	m.mu.Lock()
	if m.dtmfActive {
		m.mu.Unlock()
		return nil
	}
	if !m.started {
		m.started = true
		m.ssrc = pkt.SSRC
	}
	pkt.SSRC = m.ssrc
	pkt.SequenceNumber += m.seqOffset
	m.lastSeq = pkt.SequenceNumber
	m.lastTS = pkt.Timestamp
	m.mu.Unlock()

	return m.write(pkt)
}

func (m *sipMedia) write(pkt *rtp.Packet) error {
	s, err := m.session()
	if err != nil {
		return err
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return s.WriteRTP(pkt)
}

// SendDTMF implements calls.SIPMedia with RFC 4733 events.
func (m *sipMedia) SendDTMF(digits string) error {
	pt, ok := m.telephoneEventPT()
	if !ok {
		return errors.New("FRITZ!Box did not negotiate telephone-event")
	}
	for i, key := range digits {
		event, err := dtmfEvent(key)
		if err != nil {
			return err
		}
		if i > 0 {
			time.Sleep(dtmfGap)
		}
		if err := m.sendEvent(event, pt); err != nil {
			return err
		}
	}
	return nil
}

func (m *sipMedia) sendEvent(event uint8, pt uint8) error {
	m.mu.Lock()
	m.dtmfActive = true
	if !m.started {
		m.started = true
		m.ssrc = 0x486F6D65 // "Home" – no audio sent yet
	}
	ts := m.lastTS + codec.SamplesPerPacket
	ssrc := m.ssrc
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.dtmfActive = false
		m.mu.Unlock()
	}()

	pkts := dtmfPackets(event, pt, ts)
	for i, pkt := range pkts {
		m.mu.Lock()
		m.lastSeq++
		m.seqOffset++
		pkt.SequenceNumber = m.lastSeq
		pkt.SSRC = ssrc
		m.mu.Unlock()

		if err := m.write(pkt); err != nil {
			return err
		}
		if i < len(pkts)-dtmfEndRepeats {
			time.Sleep(dtmfPacketEvery)
		}
	}
	return nil
}

func (m *sipMedia) telephoneEventPT() (uint8, bool) {
	sess := m.med.MediaSession()
	if sess == nil {
		return 0, false
	}
	for _, c := range sess.CommonCodecs() {
		if strings.EqualFold(c.Name, "telephone-event") && c.SampleRate == 8000 {
			return c.PayloadType, true
		}
	}
	return 0, false
}
