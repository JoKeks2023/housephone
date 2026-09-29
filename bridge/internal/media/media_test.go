package media

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/testdevice"
)

func freeUDPPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func newTestEngine(t *testing.T, publicIP string) *Engine {
	t.Helper()
	e, err := NewEngine(EngineConfig{
		UDPPort:         freeUDPPort(t),
		PublicIP:        func() string { return publicIP },
		IncludeLoopback: true,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func connect(t *testing.T, e *Engine, offered []codec.Codec, deviceCodecs ...codec.Codec) (calls.Peer, *testdevice.Peer, chan calls.PeerState) {
	t.Helper()
	states := make(chan calls.PeerState, 16)
	p, err := e.NewPeer(offered, func(s calls.PeerState) { states <- s })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	offer, err := p.CreateOffer(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := testdevice.NewPeer(deviceCodecs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	answer, err := dev.Answer(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}
	if err := dev.WaitConnected(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	return p, dev, states
}

func TestOfferAdvertisesPublicIPAndOnlyOfferedCodecs(t *testing.T) {
	e := newTestEngine(t, "203.0.113.7")
	p, err := e.NewPeer([]codec.Codec{codec.PCMA}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	offer, err := p.CreateOffer(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(offer, "203.0.113.7") || !strings.Contains(offer, "typ srflx") {
		t.Fatalf("public IP not advertised as srflx:\n%s", offer)
	}
	if !strings.Contains(offer, "typ host") {
		t.Fatalf("host candidates missing:\n%s", offer)
	}
	if !strings.Contains(offer, "PCMA/8000") || strings.Contains(offer, "G722") || strings.Contains(offer, "opus") {
		t.Fatalf("offer must contain only PCMA:\n%s", offer)
	}
}

func TestRTPFlowsBothWaysWithG722(t *testing.T) {
	e := newTestEngine(t, "")
	p, dev, states := connect(t, e, []codec.Codec{codec.G722}, codec.G722)

	select {
	case s := <-states:
		if s != calls.PeerConnected {
			t.Fatalf("first state %v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no connected state")
	}
	if c, _ := p.Codec(); c != codec.G722 {
		t.Fatalf("codec %v", c)
	}

	// Bridge → device.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for i := 0; ctx.Err() == nil; i++ {
			_ = p.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(i), Timestamp: uint32(i * 160), SSRC: 7}, Payload: []byte("bridge")})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	pkt, remote, err := dev.ReadAudio(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pkt.Payload, []byte("bridge")) || pkt.PayloadType != 9 {
		t.Fatalf("device got %+v %q", pkt.Header, pkt.Payload)
	}
	if !strings.EqualFold(remote.Codec().MimeType, "audio/G722") {
		t.Fatalf("device track codec %s", remote.Codec().MimeType)
	}

	// Device → bridge.
	go dev.SendAudio(ctx, 200, []byte("device"))
	got := make(chan *rtp.Packet, 1)
	go func() {
		pkt, err := p.ReadRTP()
		if err == nil {
			got <- pkt
		}
	}()
	select {
	case pkt := <-got:
		if !bytes.Equal(pkt.Payload, []byte("device")) {
			t.Fatalf("bridge got %q", pkt.Payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge received no RTP")
	}
}

func TestDeviceChoosesCodecFromMultiCodecOffer(t *testing.T) {
	e := newTestEngine(t, "")
	// The device only supports PCMU although G722 and PCMA are preferred.
	p, dev, _ := connect(t, e, codec.Preferred, codec.PCMU)
	if c, _ := p.Codec(); c != codec.PCMU {
		t.Fatalf("codec %v, want PCMU", c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for i := 0; ctx.Err() == nil; i++ {
			_ = p.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(i), Timestamp: uint32(i * 160)}, Payload: []byte{0xff}})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	pkt, _, err := dev.ReadAudio(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.PayloadType != 0 {
		t.Fatalf("payload type %d, want 0 (PCMU)", pkt.PayloadType)
	}
}

func TestICERestartOfferKeepsConnection(t *testing.T) {
	e := newTestEngine(t, "")
	p, dev, _ := connect(t, e, []codec.Codec{codec.PCMA}, codec.PCMA)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	offer, err := p.CreateOffer(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := dev.Answer(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetAnswer(answer); err != nil {
		t.Fatal(err)
	}
	go func() {
		for i := 0; ctx.Err() == nil; i++ {
			_ = p.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(i), Timestamp: uint32(i * 160)}, Payload: []byte{1}})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	if _, _, err := dev.ReadAudio(5 * time.Second); err != nil {
		t.Fatalf("no media after ICE restart: %v", err)
	}
}

func TestFirstAudioCodec(t *testing.T) {
	answer := "v=0\r\no=- 1 1 IN IP4 0.0.0.0\r\ns=-\r\nt=0 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 8 9\r\nc=IN IP4 0.0.0.0\r\na=rtpmap:8 PCMA/8000\r\na=rtpmap:9 G722/8000\r\n"
	c, err := firstAudioCodec(answer, codec.Preferred)
	if err != nil || c != codec.PCMA {
		t.Fatalf("got %v %v", c, err)
	}
	if _, err := firstAudioCodec(answer, []codec.Codec{codec.PCMU}); err == nil {
		t.Fatal("expected error for unoffered codecs")
	}
	rejected := strings.Replace(answer, "m=audio 9", "m=audio 0", 1)
	if _, err := firstAudioCodec(rejected, codec.Preferred); err == nil {
		t.Fatal("expected error for rejected audio")
	}
}

func TestIPFilter(t *testing.T) {
	for ip, want := range map[string]bool{
		"192.168.178.20": true, "10.0.0.1": true, "169.254.1.1": false,
		"fe80::1": false, "fd00::1": false, "2a02:8108::1": true,
	} {
		if got := ipFilter(net.ParseIP(ip)); got != want {
			t.Errorf("ipFilter(%s) = %v, want %v", ip, got, want)
		}
	}
	f := interfaceFilter(nil)
	if f("docker0") || f("veth123") || !f("eth0") || !f("en0") {
		t.Error("default interface filter wrong")
	}
	if g := interfaceFilter([]string{"eth1"}); g("eth0") || !g("eth1") {
		t.Error("explicit interface filter wrong")
	}
}
