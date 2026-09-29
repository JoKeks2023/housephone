package calls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

const waitTimeout = 2 * time.Second

// fakeConn records messages sent to a device.
type fakeConn struct {
	id    string
	out   chan protocol.Envelope
	audio chan []byte

	mu   sync.Mutex
	sink AudioSink
}

func newFakeConn(id string) *fakeConn {
	return &fakeConn{id: id, out: make(chan protocol.Envelope, 100), audio: make(chan []byte, 100)}
}

func (f *fakeConn) DeviceID() string { return f.id }

func (f *fakeConn) SendAudio(frame []byte) {
	select {
	case f.audio <- frame:
	default:
	}
}

func (f *fakeConn) SetAudioSink(s AudioSink) {
	f.mu.Lock()
	f.sink = s
	f.mu.Unlock()
}

func (f *fakeConn) RemoveAudioSink(s AudioSink) {
	f.mu.Lock()
	if f.sink == s {
		f.sink = nil
	}
	f.mu.Unlock()
}

// deviceAudio simulates a binary frame from the device.
func (f *fakeConn) deviceAudio(frame []byte) {
	f.mu.Lock()
	s := f.sink
	f.mu.Unlock()
	if s != nil {
		s.DeviceAudio(frame)
	}
}

func (f *fakeConn) hasSink() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sink != nil
}
func (f *fakeConn) Send(env protocol.Envelope) {
	select {
	case f.out <- env:
	default:
		panic("fakeConn buffer full")
	}
}

// expect waits for the next message and requires its type.
func (f *fakeConn) expect(t *testing.T, msgType string, payload any) {
	t.Helper()
	select {
	case env := <-f.out:
		if env.Type != msgType {
			t.Fatalf("%s: got %s %s, want %s", f.id, env.Type, env.Payload, msgType)
		}
		if payload != nil {
			if err := env.Decode(payload); err != nil {
				t.Fatal(err)
			}
		}
	case <-time.After(waitTimeout):
		t.Fatalf("%s: timeout waiting for %s", f.id, msgType)
	}
}

func (f *fakeConn) expectEnded(t *testing.T, reason string) protocol.CallEnded {
	t.Helper()
	var ended protocol.CallEnded
	f.expect(t, protocol.TypeCallEnded, &ended)
	if ended.Reason != reason {
		t.Fatalf("%s: ended with %q, want %q", f.id, ended.Reason, reason)
	}
	return ended
}

func (f *fakeConn) expectNothing(t *testing.T) {
	t.Helper()
	select {
	case env := <-f.out:
		t.Fatalf("%s: unexpected %s %s", f.id, env.Type, env.Payload)
	case <-time.After(50 * time.Millisecond):
	}
}

// fakeMedia is SIP-side RTP.
type fakeMedia struct {
	in     chan *rtp.Packet
	out    chan *rtp.Packet
	dtmf   chan string
	closed chan struct{}
	once   sync.Once
}

func newFakeMedia() *fakeMedia {
	return &fakeMedia{in: make(chan *rtp.Packet, 10), out: make(chan *rtp.Packet, 10), dtmf: make(chan string, 10), closed: make(chan struct{})}
}

func (m *fakeMedia) ReadRTP(buf []byte, pkt *rtp.Packet) (int, error) {
	select {
	case p := <-m.in:
		*pkt = *p
		return 12 + len(p.Payload), nil
	case <-m.closed:
		return 0, io.EOF
	}
}

func (m *fakeMedia) WriteRTP(pkt *rtp.Packet) error {
	cp := *pkt
	m.out <- &cp
	return nil
}

func (m *fakeMedia) SendDTMF(digits string) error { m.dtmf <- digits; return nil }
func (m *fakeMedia) Close()                       { m.once.Do(func() { close(m.closed) }) }

// fakeSIPIn is an incoming INVITE.
type fakeSIPIn struct {
	caller string
	codec  codec.Codec
	// offered lists the codecs of the INVITE; nil means [codec, PCMA].
	offered []codec.Codec
	media   *fakeMedia
	// answeredWith is the codec of the 200 OK.
	answeredWith codec.Codec

	// answerGate, if non-nil, blocks Answer until closed.
	answerGate chan struct{}
	answerErr  error

	mu        sync.Mutex
	ringing   int
	answered  bool
	rejected  int
	hangups   int
	endReason SIPEndReason
	done      chan struct{}
	endOnce   sync.Once
	events    chan string
}

func newFakeSIPIn(caller string, c codec.Codec) *fakeSIPIn {
	return &fakeSIPIn{caller: caller, codec: c, media: newFakeMedia(), done: make(chan struct{}), events: make(chan string, 20)}
}

func (s *fakeSIPIn) Caller() string     { return s.caller }
func (s *fakeSIPIn) CallerName() string { return "Oma" }
func (s *fakeSIPIn) Codec() codec.Codec { return s.codec }
func (s *fakeSIPIn) Offers(c codec.Codec) bool {
	offered := s.offered
	if offered == nil {
		offered = []codec.Codec{s.codec, codec.PCMA}
	}
	for _, o := range offered {
		if o == c {
			return true
		}
	}
	return false
}

func (s *fakeSIPIn) answerCodec() codec.Codec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.answeredWith
}

func (s *fakeSIPIn) Ringing() error {
	s.mu.Lock()
	s.ringing++
	s.mu.Unlock()
	s.events <- "180"
	return nil
}

func (s *fakeSIPIn) Answer(ctx context.Context, c codec.Codec) (SIPMedia, error) {
	if s.answerGate != nil {
		select {
		case <-s.answerGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.answerErr != nil {
		return nil, s.answerErr
	}
	if !s.Offers(c) {
		return nil, fmt.Errorf("codec %s not offered", c)
	}
	s.mu.Lock()
	s.answered = true
	s.answeredWith = c
	s.mu.Unlock()
	s.events <- "200"
	return s.media, nil
}

func (s *fakeSIPIn) Reject(status int, reason string) error {
	s.mu.Lock()
	s.rejected = status
	s.mu.Unlock()
	s.events <- fmt.Sprint(status)
	s.end(SIPEndFailed)
	return nil
}

func (s *fakeSIPIn) Hangup(ctx context.Context) error {
	s.mu.Lock()
	s.hangups++
	s.mu.Unlock()
	s.events <- "BYE"
	s.end(SIPEndRemoteHangup)
	return nil
}

func (s *fakeSIPIn) end(r SIPEndReason) {
	s.endOnce.Do(func() {
		s.mu.Lock()
		s.endReason = r
		s.mu.Unlock()
		s.media.Close()
		close(s.done)
	})
}

// remoteEnd simulates CANCEL or BYE from the FRITZ!Box.
func (s *fakeSIPIn) remoteEnd(r SIPEndReason) { s.end(r) }

func (s *fakeSIPIn) Done() <-chan struct{} { return s.done }
func (s *fakeSIPIn) EndReason() SIPEndReason {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endReason
}

func (s *fakeSIPIn) expectEvent(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-s.events:
		if got != want {
			t.Fatalf("SIP event %q, want %q", got, want)
		}
	case <-time.After(waitTimeout):
		t.Fatalf("timeout waiting for SIP %s", want)
	}
}

// fakeSIPLeg scripts outgoing calls.
type fakeSIPLeg struct {
	registered bool
	dial       func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error)
	dials      chan string
}

func (l *fakeSIPLeg) Registered() bool { return l.registered }
func (l *fakeSIPLeg) Dial(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
	l.dials <- number + "/" + string(c)
	return l.dial(ctx, number, c, ev)
}

type fakeSIPOut struct {
	done    chan struct{}
	once    sync.Once
	hangups chan struct{}
}

func newFakeSIPOut() *fakeSIPOut {
	return &fakeSIPOut{done: make(chan struct{}), hangups: make(chan struct{}, 5)}
}

func (o *fakeSIPOut) Hangup(ctx context.Context) error {
	o.hangups <- struct{}{}
	o.remoteEnd()
	return nil
}
func (o *fakeSIPOut) remoteEnd()            { o.once.Do(func() { close(o.done) }) }
func (o *fakeSIPOut) Done() <-chan struct{} { return o.done }

// fakePeer is a device WebRTC peer. Answer SDP "codec:PCMA" selects PCMA.
type fakePeer struct {
	codecs  []codec.Codec
	onState func(PeerState)

	mu       sync.Mutex
	offers   []bool // iceRestart flags
	answer   string
	closed   bool
	fromDev  chan *rtp.Packet
	toDev    chan *rtp.Packet
	closedCh chan struct{}
}

func (p *fakePeer) CreateOffer(ctx context.Context, iceRestart bool) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.offers = append(p.offers, iceRestart)
	return fmt.Sprintf("offer-%d-restart=%v-%v", len(p.offers), iceRestart, p.codecs), nil
}

func (p *fakePeer) SetAnswer(sdp string) error {
	if sdp == "invalid" {
		return errors.New("bad sdp")
	}
	p.mu.Lock()
	p.answer = sdp
	p.mu.Unlock()
	return nil
}

func (p *fakePeer) Codec() (codec.Codec, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if name, ok := strings.CutPrefix(p.answer, "codec:"); ok {
		if c, ok := codec.FromName(name); ok {
			return c, nil
		}
		return "", errors.New("no codec")
	}
	return p.codecs[0], nil
}

func (p *fakePeer) ReadRTP() (*rtp.Packet, error) {
	select {
	case pkt := <-p.fromDev:
		return pkt, nil
	case <-p.closedCh:
		return nil, io.EOF
	}
}

func (p *fakePeer) WriteRTP(pkt *rtp.Packet) error {
	cp := *pkt
	select {
	case p.toDev <- &cp:
	default:
	}
	return nil
}

func (p *fakePeer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.closedCh)
	}
	return nil
}

func (p *fakePeer) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *fakePeer) offerFlags() []bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool(nil), p.offers...)
}

type fakePeerFactory struct {
	mu    sync.Mutex
	peers []*fakePeer
	fail  bool
}

func (f *fakePeerFactory) NewPeer(codecs []codec.Codec, onState func(PeerState)) (Peer, error) {
	if f.fail {
		return nil, errors.New("no peer for you")
	}
	p := &fakePeer{codecs: codecs, onState: onState, fromDev: make(chan *rtp.Packet, 10), toDev: make(chan *rtp.Packet, 10), closedCh: make(chan struct{})}
	f.mu.Lock()
	f.peers = append(f.peers, p)
	f.mu.Unlock()
	return p, nil
}

func (f *fakePeerFactory) peer(t *testing.T, i int) *fakePeer {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if len(f.peers) > i {
			p := f.peers[i]
			f.mu.Unlock()
			return p
		}
		f.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("peer %d not created", i)
	return nil
}

type fakePusher struct {
	pushes chan protocol.PushIncomingCall
	err    error
}

func (p *fakePusher) PushIncomingCall(ctx context.Context, dev store.Device, payload protocol.PushIncomingCall) error {
	p.pushes <- payload
	return p.err
}

type fakeDirectory struct {
	mu      sync.Mutex
	devices []store.Device
	cleared []string
}

func (d *fakeDirectory) List() ([]store.Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]store.Device(nil), d.devices...), nil
}

func (d *fakeDirectory) Get(id string) (store.Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, dev := range d.devices {
		if dev.ID == id {
			return dev, nil
		}
	}
	return store.Device{}, store.ErrDeviceNotFound
}

func (d *fakeDirectory) ClearPushToken(id, token string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cleared = append(d.cleared, id)
	return nil
}
