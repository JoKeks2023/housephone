package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/emiago/diago"
	"github.com/google/uuid"
	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// End-to-end tests for the watch (signaling v1.1): pairing and push token
// over HTTPS, WebSocket only during the call, audio as binary PCMA frames.

const watchTopic = "com.jorisconrad.housephone.watchkitapp.voip"

type watchDevice struct {
	t      *testing.T
	w      *world
	auth   string
	conn   *websocket.Conn
	msgs   chan protocol.Envelope
	frames chan []byte
}

func (w *world) httpJSON(t *testing.T, method, path, auth string, body any, out any) int {
	t.Helper()
	data, _ := json.Marshal(body)
	req, err := http.NewRequest(method, "http://"+w.bridge.Addr()+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return res.StatusCode
}

// pairWatch pairs over HTTPS and registers the push token, as the watch app
// does outside a call.
func (w *world) pairWatch(t *testing.T) *watchDevice {
	t.Helper()
	pc, err := w.bridge.Pairing.Create("Apple Watch", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var ok protocol.PairOK
	if status := w.httpJSON(t, http.MethodPost, "/v1/pair", "", protocol.Pair{Code: pc.Code, DeviceName: "Watch", Platform: protocol.PlatformWatchOS}, &ok); status != http.StatusOK {
		t.Fatalf("pair: %d", status)
	}
	auth := "Bearer " + ok.DeviceID + "." + ok.DeviceSecret
	token, env, topic := "0badc0de", protocol.PushEnvironmentDevelopment, watchTopic
	update := protocol.DeviceUpdate{PushToken: &token, PushEnvironment: &env, PushTopic: &topic, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}}
	if status := w.httpJSON(t, http.MethodPut, "/v1/device", auth, update, nil); status != http.StatusNoContent {
		t.Fatalf("device update: %d", status)
	}
	dev, err := w.bridge.Devices.Get(ok.DeviceID)
	if err != nil || dev.PushTopic != watchTopic || !dev.UsesWebSocketAudio() || dev.Name != "Apple Watch" {
		t.Fatalf("watch device %+v %v", dev, err)
	}
	return &watchDevice{t: t, w: w, auth: auth}
}

// connect opens the WebSocket, as the watch may only do during a call.
func (d *watchDevice) connect() {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, d.w.url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {d.auth}}})
	if err != nil {
		d.t.Fatal(err)
	}
	d.conn = conn
	d.msgs = make(chan protocol.Envelope, 50)
	d.frames = make(chan []byte, 200)
	go func() {
		for {
			typ, data, err := conn.Read(context.Background())
			if err != nil {
				close(d.msgs)
				return
			}
			if typ == websocket.MessageBinary {
				select {
				case d.frames <- data:
				default:
				}
				continue
			}
			if env, err := protocol.ParseEnvelope(data); err == nil {
				d.msgs <- env
			}
		}
	}()
	d.t.Cleanup(func() { conn.CloseNow() })
	d.send(protocol.TypeHello, protocol.Hello{AppVersion: "e2e-watch", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}, PushTopic: watchTopic})
	d.expect(protocol.TypeWelcome, nil)
}

func (d *watchDevice) send(msgType string, payload any) {
	d.t.Helper()
	data, _ := protocol.MustEnvelope(msgType, payload).Marshal()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := d.conn.Write(ctx, websocket.MessageText, data); err != nil {
		d.t.Fatal(err)
	}
}

func (d *watchDevice) sendFrame(fill byte) {
	d.t.Helper()
	frame := append([]byte{protocol.AudioFrameType}, bytes.Repeat([]byte{fill}, protocol.AudioFrameBytes)...)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := d.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		d.t.Fatal(err)
	}
}

func (d *watchDevice) expect(msgType string, payload any) {
	d.t.Helper()
	for {
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
			return
		case <-time.After(wait):
			d.t.Fatalf("timeout waiting for %s", msgType)
		}
	}
}

// awaitFrame waits for a binary frame whose payload is all fill.
func (d *watchDevice) awaitFrame(fill byte) {
	d.t.Helper()
	want := append([]byte{protocol.AudioFrameType}, bytes.Repeat([]byte{fill}, protocol.AudioFrameBytes)...)
	deadline := time.After(wait)
	for {
		select {
		case f := <-d.frames:
			if len(f) != protocol.AudioFrameLen || f[0] != protocol.AudioFrameType {
				d.t.Fatalf("malformed frame of %d bytes", len(f))
			}
			if bytes.Equal(f, want) {
				return
			}
		case <-deadline:
			d.t.Fatalf("no audio frame with 0x%02x", fill)
		}
	}
}

// pumpPCMA writes 20 ms PCMA packets (160 bytes of fill) until stop.
func pumpPCMA(stop chan struct{}, write func(*rtp.Packet) error, fill byte) {
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			_ = write(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: uint16(i), Timestamp: uint32(i * 160), SSRC: 0xF17}, Payload: bytes.Repeat([]byte{fill}, 160)})
		}
	}()
}

type arrival struct {
	pkt *rtp.Packet
	at  time.Time
}

// collectFills reads packets at the FRITZ!Box in the background until one
// packet per fill byte arrived, in order.
func collectFills(read func() (*rtp.Packet, error), fills []byte) <-chan []arrival {
	out := make(chan []arrival, 1)
	go func() {
		var got []arrival
		for len(got) < len(fills) {
			pkt, err := read()
			if err != nil {
				return
			}
			if len(pkt.Payload) == 160 && pkt.Payload[0] == fills[len(got)] {
				got = append(got, arrival{pkt, time.Now()})
			}
		}
		out <- got
	}()
	return out
}

func awaitArrivals(t *testing.T, ch <-chan []arrival) []arrival {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(wait):
		t.Fatal("watch audio did not reach the FRITZ!Box")
	}
	return nil
}

// checkPaced verifies that a burst of frames from the watch reached the
// FRITZ!Box as one continuous, paced RTP stream.
func checkPaced(t *testing.T, got []arrival) {
	t.Helper()
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1].pkt, got[i].pkt
		if cur.PayloadType != 8 || cur.SSRC != prev.SSRC || cur.SequenceNumber != prev.SequenceNumber+1 {
			t.Fatalf("packet %d: pt %d ssrc %x seq %d after ssrc %x seq %d", i, cur.PayloadType, cur.SSRC, cur.SequenceNumber, prev.SSRC, prev.SequenceNumber)
		}
		if step := cur.Timestamp - prev.Timestamp; step == 0 || step%160 != 0 {
			t.Fatalf("packet %d: timestamp step %d", i, step)
		}
	}
	// A burst of n frames is sent on the 20 ms clock: about (n-1)·20 ms.
	span := got[len(got)-1].at.Sub(got[0].at)
	if min := time.Duration(len(got)-1) * 20 * time.Millisecond * 3 / 4; span < min {
		t.Fatalf("burst of %d frames arrived within %v, not paced (want ≥ %v)", len(got), span, min)
	}
}

func TestEndToEndWatchIncomingCall(t *testing.T) {
	w := startWorld(t)
	watch := w.pairWatch(t)

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

	// Offline watch: woken by push, then it connects and attaches.
	var push protocol.PushIncomingCall
	select {
	case push = <-w.pusher.pushes:
	case <-time.After(wait):
		t.Fatal("watch not pushed")
	}
	// While ringing, the watch can only use HTTPS.
	if st := watch.callStatus(push.CallID); st.State != protocol.CallStatusRinging {
		t.Fatalf("status while ringing %+v", st)
	}
	watch.connect()
	watch.send(protocol.TypeCallAttach, protocol.CallAttach{CallID: push.CallID})
	var incoming protocol.CallIncoming
	watch.expect(protocol.TypeCallIncoming, &incoming)
	if incoming.Caller != "0301234567" || incoming.CallerName != "Oma" {
		t.Fatalf("incoming %+v", incoming)
	}
	var media protocol.CallMedia
	watch.expect(protocol.TypeCallMedia, &media)
	if media != protocol.NewCallMedia(push.CallID) {
		t.Fatalf("call.media %+v", media)
	}
	watch.send(protocol.TypeCallAccept, protocol.CallAccept{CallID: push.CallID})

	var res result
	select {
	case res = <-answered:
	case <-time.After(wait):
		t.Fatal("FRITZ!Box call not answered")
	}
	if res.err != nil {
		t.Fatal(res.err)
	}
	if answer := string(res.d.InviteResponse.Body()); !strings.Contains(answer, "RTP/AVP 8 101") || strings.Contains(answer, "G722") {
		t.Fatalf("200 OK must carry PCMA for the watch:\n%s", answer)
	}
	var state protocol.CallState
	watch.expect(protocol.TypeCallState, &state)
	if state.State != protocol.CallStateConnected {
		t.Fatalf("state %q", state.State)
	}

	stop := make(chan struct{})
	defer close(stop)
	// FRITZ!Box → watch: 20 ms frames.
	pumpPCMA(stop, res.med.RTPSession().WriteRTP, 0x5A)
	watch.awaitFrame(0x5A)

	// Watch → FRITZ!Box: a TCP burst of 10 frames becomes a paced stream.
	fills := []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19}
	arrivals := collectFills(sipReader(res.med), fills)
	for _, f := range fills {
		watch.sendFrame(f)
	}
	checkPaced(t, awaitArrivals(t, arrivals))

	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := res.d.Hangup(ctx); err != nil {
		t.Fatal(err)
	}
	var ended protocol.CallEnded
	watch.expect(protocol.TypeCallEnded, &ended)
	if ended.Reason != protocol.EndReasonRemoteHangup {
		t.Fatalf("ended %+v", ended)
	}
	if st := watch.awaitEndedStatus(push.CallID); st.Reason != protocol.EndReasonRemoteHangup {
		t.Fatalf("status after hangup %+v", st)
	}
}

// callStatus polls GET /v1/calls/{callId} like the ringing watch does.
func (d *watchDevice) callStatus(callID string) protocol.CallStatus {
	d.t.Helper()
	var st protocol.CallStatus
	if status := d.w.httpJSON(d.t, http.MethodGet, "/v1/calls/"+callID, d.auth, nil, &st); status != http.StatusOK {
		d.t.Fatalf("call status: HTTP %d", status)
	}
	return st
}

func (d *watchDevice) awaitEndedStatus(callID string) protocol.CallStatus {
	d.t.Helper()
	deadline := time.Now().Add(wait)
	for {
		st := d.callStatus(callID)
		if st.State == protocol.CallStatusEnded {
			return st
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("call still %q", st.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestEndToEndWatchLearnsAboutCancelWhileRinging(t *testing.T) {
	w := startWorld(t)
	watch := w.pairWatch(t)

	callCtx, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	callDone := make(chan struct{})
	go func() {
		defer close(callDone)
		_, _, _ = w.box.Call(callCtx, "0301234567", "Oma")
	}()

	var push protocol.PushIncomingCall
	select {
	case push = <-w.pusher.pushes:
	case <-time.After(wait):
		t.Fatal("watch not pushed")
	}
	if st := watch.callStatus(push.CallID); st.State != protocol.CallStatusRinging {
		t.Fatalf("status while ringing %+v", st)
	}

	// The caller hangs up before the watch was answered; the watch never
	// opened a WebSocket and learns it only through HTTPS.
	hangUp()
	if st := watch.awaitEndedStatus(push.CallID); st.Reason != protocol.EndReasonRemoteCancelled {
		t.Fatalf("status after CANCEL %+v", st)
	}
	select {
	case <-callDone:
	case <-time.After(wait):
		t.Fatal("FRITZ!Box call did not end")
	}
}

func TestEndToEndWatchOutgoingCall(t *testing.T) {
	w := startWorld(t)
	watch := w.pairWatch(t)
	watch.connect()

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
	watch.send(protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "+4930999888"})
	watch.expect(protocol.TypeCallMedia, nil)
	select {
	case invite := <-w.box.Invites:
		if body := string(invite.Body()); !strings.Contains(body, "RTP/AVP 8 101") || strings.Contains(body, "G722") {
			t.Fatalf("INVITE from the watch must offer only PCMA (+telephone-event):\n%s", body)
		}
	case <-time.After(wait):
		t.Fatal("no INVITE at the FRITZ!Box")
	}
	var state protocol.CallState
	watch.expect(protocol.TypeCallState, &state)
	if state.State != protocol.CallStateRinging {
		t.Fatalf("state %q, want ringing", state.State)
	}
	watch.expect(protocol.TypeCallState, &state)
	if state.State != protocol.CallStateConnected {
		t.Fatalf("state %q, want connected", state.State)
	}
	<-answered
	mu.Lock()
	med := boxMedia
	mu.Unlock()

	stop := make(chan struct{})
	defer close(stop)
	fills := []byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25}
	arrivals := collectFills(sipReader(med), fills)
	for _, f := range fills {
		watch.sendFrame(f)
	}
	checkPaced(t, awaitArrivals(t, arrivals))
	pumpPCMA(stop, med.RTPSession().WriteRTP, 0x6B)
	watch.awaitFrame(0x6B)

	watch.send(protocol.TypeCallHangup, protocol.CallHangup{CallID: callID, Reason: protocol.HangupReasonHangup})
	var ended protocol.CallEnded
	watch.expect(protocol.TypeCallEnded, &ended)
	if ended.Reason != protocol.EndReasonLocalHangup {
		t.Fatalf("ended %+v", ended)
	}
	select {
	case <-boxEnded:
	case <-time.After(wait):
		t.Fatal("FRITZ!Box did not receive BYE")
	}
}
