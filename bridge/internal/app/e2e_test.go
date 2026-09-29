package app_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/emiago/diago"
	"github.com/google/uuid"
	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/fakefritz"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
	"github.com/JoKeks2023/housephone/bridge/internal/testdevice"
)

const wait = 10 * time.Second

type recordingPusher struct {
	pushes chan protocol.PushIncomingCall
}

func (p *recordingPusher) PushIncomingCall(ctx context.Context, dev store.Device, payload protocol.PushIncomingCall) error {
	p.pushes <- payload
	return nil
}

func freeUDP(t *testing.T) int {
	t.Helper()
	port, err := fakefritz.FreeUDPPort()
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func logger() *slog.Logger {
	if os.Getenv("E2E_LOG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type world struct {
	box    *fakefritz.Box
	bridge *app.Bridge
	pusher *recordingPusher
	url    string
}

// startWorld starts a fake FRITZ!Box and a bridge; opts adjust the bridge
// config before it starts.
func startWorld(t *testing.T, opts ...func(*config.Config)) *world {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	box, err := fakefritz.Start(ctx, "620", "geheim", nil, logger())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Bridge.Listen = "127.0.0.1:0"
	cfg.Bridge.DataDir = t.TempDir()
	cfg.SIP.Registrar = box.Host
	cfg.SIP.Port = box.Port
	cfg.SIP.Username = "620"
	cfg.SIP.Password = "geheim"
	cfg.SIP.BindHost = "127.0.0.1"
	cfg.SIP.BindPort = freeUDP(t)
	cfg.Media.UDPPort = freeUDP(t)
	cfg.Media.DetectPublicIP = false
	cfg.Media.STUN = nil
	cfg.Media.IncludeLoopback = true
	cfg.Media.Interfaces = []string{"lo0", "lo"}
	for _, opt := range opts {
		opt(&cfg)
	}
	pusher := &recordingPusher{pushes: make(chan protocol.PushIncomingCall, 10)}
	bridge, err := app.New(ctx, cfg, logger(), app.WithPusher(pusher))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := bridge.Run(ctx); err != nil {
			t.Errorf("bridge: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("bridge did not shut down")
		}
	})
	w := &world{box: box, bridge: bridge, pusher: pusher, url: "ws://" + bridge.Addr() + "/v1/ws"}
	deadline := time.Now().Add(wait)
	for !bridge.SIPRegistered() {
		if time.Now().After(deadline) {
			t.Fatal("bridge did not register at the fake FRITZ!Box")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return w
}

// device is a test app: WebSocket client plus WebRTC peer.
type device struct {
	t    *testing.T
	conn *websocket.Conn
	msgs chan protocol.Envelope
	// auth is the Authorization header value (Bearer) of this device.
	auth string
	// welcome is the bridge's answer to hello.
	welcome protocol.Welcome
}

func (w *world) pairAndConnect(t *testing.T) *device {
	t.Helper()
	pc, err := w.bridge.Pairing.Create("Test-iPhone", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	pairConn, _, err := websocket.Dial(ctx, w.url, nil)
	if err != nil {
		t.Fatal(err)
	}
	pd := &device{t: t, conn: pairConn, msgs: make(chan protocol.Envelope, 50)}
	go pd.readLoop()
	pd.send(protocol.TypePair, protocol.Pair{Code: pc.Code, DeviceName: "iPhone", Platform: protocol.PlatformIOS})
	var ok protocol.PairOK
	pd.expect(protocol.TypePairOK, &ok)
	pairConn.CloseNow()

	conn, _, err := websocket.Dial(ctx, w.url, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": {"Bearer " + ok.DeviceID + "." + ok.DeviceSecret},
	}})
	if err != nil {
		t.Fatal(err)
	}
	d := &device{t: t, conn: conn, msgs: make(chan protocol.Envelope, 50), auth: "Bearer " + ok.DeviceID + "." + ok.DeviceSecret}
	go d.readLoop()
	t.Cleanup(func() { conn.CloseNow() })
	d.send(protocol.TypeHello, protocol.Hello{AppVersion: "e2e", Platform: protocol.PlatformIOS, PushToken: "a1b2c3d4", PushEnvironment: protocol.PushEnvironmentDevelopment})
	d.expect(protocol.TypeWelcome, &d.welcome)
	if !d.welcome.SIPRegistered {
		t.Fatal("welcome reports no SIP registration")
	}
	return d
}

func (d *device) readLoop() {
	for {
		_, data, err := d.conn.Read(context.Background())
		if err != nil {
			close(d.msgs)
			return
		}
		env, err := protocol.ParseEnvelope(data)
		if err == nil {
			d.msgs <- env
		}
	}
}

func (d *device) send(msgType string, payload any) {
	d.t.Helper()
	data, _ := protocol.MustEnvelope(msgType, payload).Marshal()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := d.conn.Write(ctx, websocket.MessageText, data); err != nil {
		d.t.Fatal(err)
	}
}

func (d *device) expect(msgType string, payload any) {
	d.t.Helper()
	select {
	case env, ok := <-d.msgs:
		if !ok {
			d.t.Fatalf("connection closed while waiting for %s", msgType)
		}
		if env.Type != msgType {
			d.t.Fatalf("got %s %s, want %s", env.Type, env.Payload, msgType)
		}
		if payload != nil {
			if err := env.Decode(payload); err != nil {
				d.t.Fatal(err)
			}
		}
	case <-time.After(wait):
		d.t.Fatalf("timeout waiting for %s", msgType)
	}
}

// answerOffer creates the device's WebRTC answer.
func (d *device) answerOffer(callID, sdp string, codecs ...codec.Codec) *testdevice.Peer {
	d.t.Helper()
	peer, err := testdevice.NewPeer(codecs...)
	if err != nil {
		d.t.Fatal(err)
	}
	d.t.Cleanup(func() { _ = peer.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	answer, err := peer.Answer(ctx, sdp)
	if err != nil {
		d.t.Fatal(err)
	}
	d.send(protocol.TypeCallAnswer, protocol.CallAnswer{CallID: callID, SDP: answer})
	return peer
}

// pumpRTP writes packets with payload until stop is closed.
func pumpRTP(stop chan struct{}, write func(*rtp.Packet) error, pt uint8, payload string) {
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			_ = write(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: uint16(i), Timestamp: uint32(i * 160), SSRC: 0xABC}, Payload: []byte(payload)})
		}
	}()
}

// awaitRTP reads until a packet with payload arrives.
func awaitRTP(t *testing.T, read func() (*rtp.Packet, error), payload string) *rtp.Packet {
	t.Helper()
	got := make(chan *rtp.Packet, 1)
	go func() {
		for {
			pkt, err := read()
			if err != nil {
				return
			}
			if bytes.Equal(pkt.Payload, []byte(payload)) {
				got <- pkt
				return
			}
		}
	}()
	select {
	case p := <-got:
		return p
	case <-time.After(wait):
		t.Fatalf("no RTP with payload %q", payload)
	}
	return nil
}

func remoteReader(t *testing.T, peer *testdevice.Peer) func() (*rtp.Packet, error) {
	t.Helper()
	select {
	case track := <-peer.Remote:
		peer.Remote <- track
		return func() (*rtp.Packet, error) {
			pkt, _, err := track.ReadRTP()
			return pkt, err
		}
	case <-time.After(wait):
		t.Fatal("device got no remote track")
	}
	return nil
}

func sipReader(med *diago.DialogMedia) func() (*rtp.Packet, error) {
	buf := make([]byte, 1500)
	return func() (*rtp.Packet, error) {
		var pkt rtp.Packet
		s := med.RTPSession()
		if _, err := s.ReadRTP(buf, &pkt); err != nil {
			return nil, err
		}
		cp := pkt
		cp.Payload = append([]byte(nil), pkt.Payload...)
		return &cp, nil
	}
}

func TestEndToEndIncomingCall(t *testing.T) {
	w := startWorld(t)
	d := w.pairAndConnect(t)

	type result struct {
		d   *diago.DialogClientSession
		med *diago.DialogMedia
		err error
	}
	answered := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dlg, med, err := w.box.Call(ctx, "0301234567", "Oma")
		answered <- result{dlg, med, err}
	}()

	// The phone is online: it gets call.incoming and a push.
	var incoming protocol.CallIncoming
	d.expect(protocol.TypeCallIncoming, &incoming)
	if incoming.Caller != "0301234567" || incoming.CallerName != "Oma" {
		t.Fatalf("incoming %+v", incoming)
	}
	select {
	case push := <-w.pusher.pushes:
		if push.CallID != incoming.CallID || push.BridgeID != w.bridge.Identity.ID {
			t.Fatalf("push %+v", push)
		}
	case <-time.After(wait):
		t.Fatal("no push")
	}

	d.send(protocol.TypeCallAttach, protocol.CallAttach{CallID: incoming.CallID})
	d.expect(protocol.TypeCallIncoming, nil)
	var offer protocol.CallOffer
	d.expect(protocol.TypeCallOffer, &offer)
	if !strings.Contains(offer.SDP, "G722/8000") || strings.Contains(offer.SDP, "PCMA") {
		t.Fatalf("offer must carry only G722:\n%s", offer.SDP)
	}
	peer := d.answerOffer(incoming.CallID, offer.SDP, codec.G722)
	d.send(protocol.TypeCallAccept, protocol.CallAccept{CallID: incoming.CallID})

	var res result
	select {
	case res = <-answered:
	case <-time.After(wait):
		t.Fatal("FRITZ!Box call not answered")
	}
	if res.err != nil {
		t.Fatal(res.err)
	}
	var state protocol.CallState
	d.expect(protocol.TypeCallState, &state)
	if state.State != protocol.CallStateConnected {
		t.Fatalf("state %q", state.State)
	}
	if err := peer.WaitConnected(wait); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	defer close(stop)
	// FRITZ!Box → phone.
	pumpRTP(stop, res.med.RTPSession().WriteRTP, 9, "hallo vom festnetz")
	pkt := awaitRTP(t, remoteReader(t, peer), "hallo vom festnetz")
	if pkt.PayloadType != 9 {
		t.Fatalf("device received PT %d", pkt.PayloadType)
	}
	// Phone → FRITZ!Box.
	pumpRTP(stop, peer.Track.WriteRTP, 9, "hallo vom handy")
	awaitRTP(t, sipReader(res.med), "hallo vom handy")

	// The caller hangs up.
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := res.d.Hangup(ctx); err != nil {
		t.Fatal(err)
	}
	var ended protocol.CallEnded
	d.expect(protocol.TypeCallEnded, &ended)
	if ended.Reason != protocol.EndReasonRemoteHangup {
		t.Fatalf("ended %+v", ended)
	}
}

func TestEndToEndOutgoingCall(t *testing.T) {
	w := startWorld(t)
	d := w.pairAndConnect(t)

	var (
		mu       sync.Mutex
		boxMedia *diago.DialogMedia
		boxEnded = make(chan struct{})
		answered = make(chan struct{})
	)
	w.box.SetOnInvite(func(dlg *diago.DialogServerSession) {
		_ = dlg.Ringing()
		time.Sleep(200 * time.Millisecond)
		med, err := dlg.Answer(diago.AnswerOptions{})
		if err != nil {
			return
		}
		mu.Lock()
		boxMedia = med
		mu.Unlock()
		close(answered)
		<-dlg.Context().Done()
		close(boxEnded)
	})

	callID := uuid.NewString()
	d.send(protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "+4930999888"})
	var offer protocol.CallOffer
	d.expect(protocol.TypeCallOffer, &offer)
	for _, want := range []string{"G722/8000", "PCMA/8000", "PCMU/8000"} {
		if !strings.Contains(offer.SDP, want) {
			t.Fatalf("outgoing offer lacks %s:\n%s", want, offer.SDP)
		}
	}
	peer := d.answerOffer(callID, offer.SDP, codec.G722, codec.PCMA)

	select {
	case invite := <-w.box.Invites:
		if invite.Recipient.User != "+4930999888" {
			t.Fatalf("dialed %q", invite.Recipient.User)
		}
		if body := string(invite.Body()); !strings.Contains(body, "RTP/AVP 9 101") {
			t.Fatalf("INVITE must offer only G722 (+telephone-event):\n%s", body)
		}
	case <-time.After(wait):
		t.Fatal("no INVITE at the FRITZ!Box")
	}
	var state protocol.CallState
	d.expect(protocol.TypeCallState, &state)
	if state.State != protocol.CallStateRinging {
		t.Fatalf("state %q, want ringing", state.State)
	}
	d.expect(protocol.TypeCallState, &state)
	if state.State != protocol.CallStateConnected {
		t.Fatalf("state %q, want connected", state.State)
	}
	<-answered
	if err := peer.WaitConnected(wait); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	med := boxMedia
	mu.Unlock()

	stop := make(chan struct{})
	defer close(stop)
	pumpRTP(stop, peer.Track.WriteRTP, 9, "ausgehend")
	awaitRTP(t, sipReader(med), "ausgehend")
	pumpRTP(stop, med.RTPSession().WriteRTP, 9, "antwort")
	awaitRTP(t, remoteReader(t, peer), "antwort")

	// Tastentöne reach the FRITZ!Box as RFC 4733 in the audio stream.
	d.send(protocol.TypeCallDTMF, protocol.CallDTMF{CallID: callID, Digits: "7"})
	buf := make([]byte, 1500)
	gotDTMF := make(chan byte, 1)
	go func() {
		for {
			var pkt rtp.Packet
			if _, err := med.MediaSession().ReadRTP(buf, &pkt); err != nil {
				return
			}
			if pkt.PayloadType == 101 && len(pkt.Payload) >= 4 {
				gotDTMF <- pkt.Payload[0]
				return
			}
		}
	}()
	select {
	case ev := <-gotDTMF:
		if ev != 7 {
			t.Fatalf("DTMF event %d", ev)
		}
	case <-time.After(wait):
		t.Fatal("no DTMF at the FRITZ!Box")
	}

	d.send(protocol.TypeCallHangup, protocol.CallHangup{CallID: callID, Reason: protocol.HangupReasonHangup})
	var ended protocol.CallEnded
	d.expect(protocol.TypeCallEnded, &ended)
	if ended.Reason != protocol.EndReasonLocalHangup {
		t.Fatalf("ended %+v", ended)
	}
	select {
	case <-boxEnded:
	case <-time.After(wait):
		t.Fatal("FRITZ!Box did not receive BYE")
	}
}

func TestEndToEndHealthAndPairingLink(t *testing.T) {
	w := startWorld(t)
	res, err := http.Get("http://" + w.bridge.Addr() + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health %d", res.StatusCode)
	}
	link := app.PairingLink("wss://phone.example.com/v1/ws", "K7P2XH9QRM", "Mein Zuhause")
	for _, want := range []string{"housephone://pair?", "code=K7P2XH9QRM", "url=wss%3A%2F%2Fphone.example.com%2Fv1%2Fws", "name=Mein%20Zuhause"} {
		if !strings.Contains(link, want) {
			t.Fatalf("link %s lacks %s", link, want)
		}
	}
}
