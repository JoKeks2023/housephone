package calls

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// wsQueueFrames bounds the device → SIP backlog (200 ms). TCP delivers
// frames in bursts; beyond this the oldest frames are dropped so latency
// cannot build up.
const wsQueueFrames = 10

var errNoSDP = errors.New("websocket-pcma media has no SDP")

// wsPeer carries the audio of a websocket-pcma device (the watch, v1.1) as
// binary WebSocket frames: 0x01 followed by 160 bytes of A-law (20 ms).
//
// It implements Peer so the relay forwards RTP to and from it exactly like
// a WebRTC peer:
//   - WriteRTP (SIP → device) re-frames any packet size into 160-byte frames.
//   - ReadRTP (device → SIP) paces queued frames onto a 20 ms clock, because
//     frames arrive over TCP in bursts. The timestamp advances with the
//     clock, so gaps stay visible to the FRITZ!Box.
//
// Device audio is dropped until the call is connected (setLive).
type wsPeer struct {
	interval time.Duration

	mu       sync.Mutex
	conn     DeviceConn
	live     bool
	closed   bool
	closedCh chan struct{}
	queue    [][]byte
	pending  []byte
	ticker   *time.Ticker
	seq      uint16
	ts       uint32
	idle     bool
	dropped  uint64
}

func newWSPeer(interval time.Duration) *wsPeer {
	if interval <= 0 {
		interval = protocol.MediaFrameMs * time.Millisecond
	}
	return &wsPeer{interval: interval, closedCh: make(chan struct{}), idle: true}
}

// bind attaches the peer to the device's current connection (nil while the
// device is detached). Audio from the previous connection is ignored.
func (p *wsPeer) bind(conn DeviceConn) {
	p.mu.Lock()
	old := p.conn
	p.conn = conn
	closed := p.closed
	p.mu.Unlock()
	if old != nil && old != conn {
		old.RemoveAudioSink(p)
	}
	if conn != nil && !closed {
		conn.SetAudioSink(p)
	}
}

// setLive starts forwarding device audio (the call is connected).
func (p *wsPeer) setLive(live bool) {
	p.mu.Lock()
	p.live = live
	if !live {
		p.queue = nil
	}
	p.mu.Unlock()
}

// DeviceAudio implements AudioSink: one binary WebSocket message from the device.
func (p *wsPeer) DeviceAudio(frame []byte) {
	if len(frame) != protocol.AudioFrameLen || frame[0] != protocol.AudioFrameType {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.live || p.closed {
		return
	}
	if len(p.queue) >= wsQueueFrames {
		p.queue = p.queue[1:]
		p.dropped++
	}
	p.queue = append(p.queue, append([]byte(nil), frame[1:]...))
}

// CreateOffer implements Peer; websocket-pcma devices get call.media instead.
func (p *wsPeer) CreateOffer(context.Context, bool) (string, error) { return "", errNoSDP }

// SetAnswer implements Peer; websocket-pcma devices never answer.
func (p *wsPeer) SetAnswer(string) error { return errNoSDP }

// Codec implements Peer.
func (p *wsPeer) Codec() (codec.Codec, error) { return codec.PCMA, nil }

// WriteRTP sends SIP audio to the device in 20 ms frames. Without a
// connection the audio is dropped.
func (p *wsPeer) WriteRTP(pkt *rtp.Packet) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return io.ErrClosedPipe
	}
	p.pending = append(p.pending, pkt.Payload...)
	var frames [][]byte
	for len(p.pending) >= protocol.AudioFrameBytes {
		frame := make([]byte, protocol.AudioFrameLen)
		frame[0] = protocol.AudioFrameType
		copy(frame[1:], p.pending[:protocol.AudioFrameBytes])
		frames = append(frames, frame)
		p.pending = p.pending[protocol.AudioFrameBytes:]
	}
	if len(p.pending) == 0 {
		p.pending = nil
	}
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return nil
	}
	for _, f := range frames {
		conn.SendAudio(f)
	}
	return nil
}

// ReadRTP returns the next device frame on the 20 ms clock. It blocks until
// a frame is due or the peer is closed. There is only one reader (the relay).
func (p *wsPeer) ReadRTP() (*rtp.Packet, error) {
	tick, err := p.tickC()
	if err != nil {
		return nil, err
	}
	for {
		select {
		case <-p.closedCh:
			return nil, io.EOF
		case <-tick:
		}
		p.mu.Lock()
		p.ts += codec.SamplesPerPacket
		if len(p.queue) == 0 {
			p.idle = true
			p.mu.Unlock()
			continue
		}
		payload := p.queue[0]
		p.queue = p.queue[1:]
		p.seq++
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Marker:         p.idle,
				PayloadType:    codec.PCMA.PayloadType(),
				SequenceNumber: p.seq,
				Timestamp:      p.ts,
				SSRC:           1,
			},
			Payload: payload,
		}
		p.idle = false
		p.mu.Unlock()
		return pkt, nil
	}
}

func (p *wsPeer) tickC() (<-chan time.Time, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, io.EOF
	}
	if p.ticker == nil {
		p.ticker = time.NewTicker(p.interval)
	}
	return p.ticker.C, nil
}

// Close implements Peer.
func (p *wsPeer) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.closedCh)
	if p.ticker != nil {
		p.ticker.Stop()
	}
	conn := p.conn
	p.conn = nil
	p.queue = nil
	p.mu.Unlock()
	if conn != nil {
		conn.RemoveAudioSink(p)
	}
	return nil
}

// droppedFrames returns how many device frames were discarded (queue full).
func (p *wsPeer) droppedFrames() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dropped
}
