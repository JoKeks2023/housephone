package calls

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

type harness struct {
	t      *testing.T
	m      *Manager
	sip    *fakeSIPLeg
	peers  *fakePeerFactory
	pusher *fakePusher
	dir    *fakeDirectory
}

func newHarness(t *testing.T, devices ...store.Device) *harness {
	t.Helper()
	h := &harness{
		t:      t,
		sip:    &fakeSIPLeg{registered: true, dials: make(chan string, 5)},
		peers:  &fakePeerFactory{},
		pusher: &fakePusher{pushes: make(chan protocol.PushIncomingCall, 10)},
		dir:    &fakeDirectory{devices: devices},
	}
	h.m = NewManager(Options{
		BridgeID:        "bridge-1",
		ICEServers:      []protocol.ICEServer{{URLs: []string{"stun:stun.example.com:3478"}}},
		SIP:             h.sip,
		Peers:           h.peers,
		Pusher:          h.pusher,
		Devices:         h.dir,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReattachTimeout: 150 * time.Millisecond,
	})
	return h
}

func device(id, token string) store.Device {
	return store.Device{ID: id, Name: id, Platform: protocol.PlatformIOS, PushToken: token, PushEnvironment: protocol.PushEnvironmentDevelopment}
}

func (h *harness) send(conn *fakeConn, msgType string, payload any) {
	h.m.HandleDeviceMessage(conn, protocol.MustEnvelope(msgType, payload))
}

// incoming starts HandleIncoming and returns a channel closed when it returns.
func (h *harness) incoming(sip *fakeSIPIn) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.m.HandleIncoming(context.Background(), sip)
	}()
	return done
}

func (h *harness) expectPush(t *testing.T) protocol.PushIncomingCall {
	t.Helper()
	select {
	case p := <-h.pusher.pushes:
		return p
	case <-time.After(waitTimeout):
		t.Fatal("no push")
	}
	return protocol.PushIncomingCall{}
}

func waitClosed(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(waitTimeout):
		t.Fatalf("%s did not finish", what)
	}
}

// attachAndAnswer attaches conn to callID and answers the offer.
func (h *harness) attachAndAnswer(t *testing.T, conn *fakeConn, callID, answer string) protocol.CallOffer {
	t.Helper()
	h.send(conn, protocol.TypeCallAttach, protocol.CallAttach{CallID: callID})
	var incoming protocol.CallIncoming
	conn.expect(t, protocol.TypeCallIncoming, &incoming)
	if incoming.CallID != callID {
		t.Fatalf("incoming for %s, want %s", incoming.CallID, callID)
	}
	var offer protocol.CallOffer
	conn.expect(t, protocol.TypeCallOffer, &offer)
	h.send(conn, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: callID, SDP: answer})
	return offer
}

func rtpPacket(pt uint8, seq uint16) *rtp.Packet {
	return &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: seq, Timestamp: uint32(seq) * 160, SSRC: 1234}, Payload: []byte{1, 2, 3}}
}

func TestIncomingCallViaPushAcceptRelayAndHangup(t *testing.T) {
	h := newHarness(t, device("dev-a", "tok-a"))
	sip := newFakeSIPIn("+4930123456", codec.G722)
	done := h.incoming(sip)

	sip.expectEvent(t, "180")
	push := h.expectPush(t)
	if push.Type != protocol.PushTypeIncomingCall || push.Caller != "+4930123456" || push.CallerName != "Oma" || push.BridgeID != "bridge-1" {
		t.Fatalf("unexpected push %+v", push)
	}

	conn := newFakeConn("dev-a")
	h.m.DeviceConnected(conn)
	offer := h.attachAndAnswer(t, conn, push.CallID, "answer")
	if !strings.Contains(offer.SDP, "[G722]") || len(offer.ICEServers) != 1 {
		t.Fatalf("offer should contain only the SIP codec and ICE servers: %+v", offer)
	}

	h.send(conn, protocol.TypeCallAccept, protocol.CallAccept{CallID: push.CallID})
	sip.expectEvent(t, "200")
	var state protocol.CallState
	conn.expect(t, protocol.TypeCallState, &state)
	if state.State != protocol.CallStateConnected {
		t.Fatalf("state %q", state.State)
	}

	// Media: SIP → device drops telephone-event, device → SIP gets PT 9.
	peer := h.peers.peer(t, 0)
	sip.media.in <- rtpPacket(101, 1)
	sip.media.in <- rtpPacket(9, 2)
	select {
	case got := <-peer.toDev:
		if got.SequenceNumber != 2 || got.PayloadType != 9 {
			t.Fatalf("unexpected packet to device %+v", got.Header)
		}
	case <-time.After(waitTimeout):
		t.Fatal("no packet reached the device")
	}
	fromDev := rtpPacket(111, 7)
	peer.fromDev <- fromDev
	select {
	case got := <-sip.media.out:
		if got.PayloadType != 9 || got.SSRC == 1234 {
			t.Fatalf("packet to SIP not rewritten %+v", got.Header)
		}
	case <-time.After(waitTimeout):
		t.Fatal("no packet reached SIP")
	}

	h.send(conn, protocol.TypeCallDTMF, protocol.CallDTMF{CallID: push.CallID, Digits: "12#"})
	select {
	case d := <-sip.media.dtmf:
		if d != "12#" {
			t.Fatalf("dtmf %q", d)
		}
	case <-time.After(waitTimeout):
		t.Fatal("dtmf not forwarded")
	}

	h.send(conn, protocol.TypeCallHangup, protocol.CallHangup{CallID: push.CallID, Reason: protocol.HangupReasonHangup})
	conn.expectEnded(t, protocol.EndReasonLocalHangup)
	sip.expectEvent(t, "BYE")
	waitClosed(t, done, "HandleIncoming")
	if !peer.isClosed() {
		t.Fatal("peer not closed")
	}
	conn.expectNothing(t)

	// A late message for the ended call gets its tombstone reason.
	h.send(conn, protocol.TypeCallAttach, protocol.CallAttach{CallID: push.CallID})
	conn.expectEnded(t, protocol.EndReasonLocalHangup)
}

func TestIncomingMultiDeviceFirstAcceptWins(t *testing.T) {
	h := newHarness(t, device("dev-a", "tok-a"), device("dev-b", ""), device("dev-c", "tok-c"))
	a, b := newFakeConn("dev-a"), newFakeConn("dev-b")
	h.m.DeviceConnected(a)
	h.m.DeviceConnected(b)

	sip := newFakeSIPIn("0301234", codec.PCMA)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")

	// Online devices are informed directly; devices with tokens are pushed.
	var incA, incB protocol.CallIncoming
	a.expect(t, protocol.TypeCallIncoming, &incA)
	b.expect(t, protocol.TypeCallIncoming, &incB)
	h.expectPush(t)
	h.expectPush(t)
	callID := incA.CallID

	h.attachAndAnswer(t, a, callID, "answer-a")
	h.attachAndAnswer(t, b, callID, "answer-b")

	h.send(b, protocol.TypeCallAccept, protocol.CallAccept{CallID: callID})
	a.expectEnded(t, protocol.EndReasonAnsweredElsewhere)
	sip.expectEvent(t, "200")
	b.expect(t, protocol.TypeCallState, nil)

	// A late accept from A is refused, the late device C learns the outcome.
	h.send(a, protocol.TypeCallAccept, protocol.CallAccept{CallID: callID})
	a.expectNothing(t) // already told
	c := newFakeConn("dev-c")
	h.m.DeviceConnected(c)
	h.send(c, protocol.TypeCallAttach, protocol.CallAttach{CallID: callID})
	c.expectEnded(t, protocol.EndReasonAnsweredElsewhere)

	sip.remoteEnd(SIPEndRemoteHangup)
	b.expectEnded(t, protocol.EndReasonRemoteHangup)
	waitClosed(t, done, "HandleIncoming")
}

func TestIncomingAllDeclineRejectsWith486(t *testing.T) {
	h := newHarness(t, device("dev-a", ""), device("dev-b", ""))
	a, b := newFakeConn("dev-a"), newFakeConn("dev-b")
	h.m.DeviceConnected(a)
	h.m.DeviceConnected(b)
	sip := newFakeSIPIn("123", codec.PCMU)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	var inc protocol.CallIncoming
	a.expect(t, protocol.TypeCallIncoming, &inc)
	b.expect(t, protocol.TypeCallIncoming, nil)

	h.attachAndAnswer(t, a, inc.CallID, "x")
	h.send(a, protocol.TypeCallHangup, protocol.CallHangup{CallID: inc.CallID, Reason: protocol.HangupReasonDeclined})
	a.expectEnded(t, protocol.EndReasonLocalHangup)
	sip.mu.Lock()
	rejected := sip.rejected
	sip.mu.Unlock()
	if rejected != 0 {
		t.Fatal("rejected although dev-b can still answer")
	}

	// dev-b declines without ever attaching.
	h.send(b, protocol.TypeCallHangup, protocol.CallHangup{CallID: inc.CallID, Reason: protocol.HangupReasonDeclined})
	b.expectEnded(t, protocol.EndReasonLocalHangup)
	sip.expectEvent(t, "486")
	waitClosed(t, done, "HandleIncoming")
}

func TestIncomingCancelAndAnsweredElsewhere(t *testing.T) {
	for _, tc := range []struct {
		reason SIPEndReason
		want   string
	}{
		{SIPEndCancelled, protocol.EndReasonRemoteCancelled},
		{SIPEndAnsweredElsewhere, protocol.EndReasonAnsweredElsewhere},
	} {
		t.Run(tc.want, func(t *testing.T) {
			h := newHarness(t, device("dev-a", "tok"), device("dev-b", ""))
			a, b := newFakeConn("dev-a"), newFakeConn("dev-b")
			h.m.DeviceConnected(a)
			h.m.DeviceConnected(b)
			sip := newFakeSIPIn("123", codec.G722)
			done := h.incoming(sip)
			var inc protocol.CallIncoming
			a.expect(t, protocol.TypeCallIncoming, &inc)
			b.expect(t, protocol.TypeCallIncoming, nil)
			h.attachAndAnswer(t, a, inc.CallID, "x")

			sip.remoteEnd(tc.reason)
			a.expectEnded(t, tc.want) // attached
			b.expectEnded(t, tc.want) // informed only
			waitClosed(t, done, "HandleIncoming")

			// A device woken by the push attaches too late.
			late := newFakeConn("dev-a")
			h.send(late, protocol.TypeCallAttach, protocol.CallAttach{CallID: inc.CallID})
			late.expectEnded(t, tc.want)
		})
	}
}

func TestIncomingWithoutReachableDevicesRejects480(t *testing.T) {
	h := newHarness(t, device("dev-a", "")) // no token, offline
	sip := newFakeSIPIn("123", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "480")
	waitClosed(t, done, "HandleIncoming")
}

func TestIncomingInvalidPushTokenIsClearedAndCallRejected(t *testing.T) {
	h := newHarness(t, device("dev-a", "stale"))
	h.pusher.err = ErrInvalidPushToken
	sip := newFakeSIPIn("123", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	h.expectPush(t)
	sip.expectEvent(t, "480")
	waitClosed(t, done, "HandleIncoming")
	h.dir.mu.Lock()
	defer h.dir.mu.Unlock()
	if len(h.dir.cleared) != 1 || h.dir.cleared[0] != "dev-a" {
		t.Fatalf("token not cleared: %v", h.dir.cleared)
	}
}

func TestIncomingHangupWhileAnsweringSendsByeAfterAck(t *testing.T) {
	h := newHarness(t, device("dev-a", ""))
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	sip := newFakeSIPIn("123", codec.G722)
	sip.answerGate = make(chan struct{})
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	var inc protocol.CallIncoming
	a.expect(t, protocol.TypeCallIncoming, &inc)
	h.attachAndAnswer(t, a, inc.CallID, "x")
	h.send(a, protocol.TypeCallAccept, protocol.CallAccept{CallID: inc.CallID})
	h.send(a, protocol.TypeCallHangup, protocol.CallHangup{CallID: inc.CallID})
	a.expectEnded(t, protocol.EndReasonLocalHangup)
	close(sip.answerGate)
	sip.expectEvent(t, "200")
	sip.expectEvent(t, "BYE")
	waitClosed(t, done, "HandleIncoming")
}

func TestIncomingReattachWithICERestart(t *testing.T) {
	h := newHarness(t, device("dev-a", ""))
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	sip := newFakeSIPIn("123", codec.G722)
	done := h.incoming(sip)
	var inc protocol.CallIncoming
	a.expect(t, protocol.TypeCallIncoming, &inc)
	h.attachAndAnswer(t, a, inc.CallID, "x")
	h.send(a, protocol.TypeCallAccept, protocol.CallAccept{CallID: inc.CallID})
	a.expect(t, protocol.TypeCallState, nil)

	h.m.DeviceDisconnected(a)
	a2 := newFakeConn("dev-a")
	h.m.DeviceConnected(a2)
	h.send(a2, protocol.TypeCallAttach, protocol.CallAttach{CallID: inc.CallID})
	// Connected: no call.incoming, only the restart offer and the state.
	var offer protocol.CallOffer
	a2.expect(t, protocol.TypeCallOffer, &offer)
	if !strings.Contains(offer.SDP, "restart=true") {
		t.Fatalf("re-attach offer without ICE restart: %s", offer.SDP)
	}
	var state protocol.CallState
	a2.expect(t, protocol.TypeCallState, &state)
	if state.State != protocol.CallStateConnected {
		t.Fatalf("state %q", state.State)
	}
	h.send(a2, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: inc.CallID, SDP: "x2"})

	// Survives beyond the re-attach timeout.
	time.Sleep(250 * time.Millisecond)
	if h.m.ActiveCalls() != 1 {
		t.Fatal("call ended although the device re-attached")
	}
	if got := len(h.peers.peer(t, 0).offerFlags()); got != 2 {
		t.Fatalf("expected the same peer to be re-offered, offers=%d", got)
	}
	sip.remoteEnd(SIPEndRemoteHangup)
	a2.expectEnded(t, protocol.EndReasonRemoteHangup)
	waitClosed(t, done, "HandleIncoming")
}

func TestIncomingReattachTimeoutHangsUp(t *testing.T) {
	h := newHarness(t, device("dev-a", ""))
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	sip := newFakeSIPIn("123", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	var inc protocol.CallIncoming
	a.expect(t, protocol.TypeCallIncoming, &inc)
	h.attachAndAnswer(t, a, inc.CallID, "x")
	h.send(a, protocol.TypeCallAccept, protocol.CallAccept{CallID: inc.CallID})
	sip.expectEvent(t, "200")

	h.m.DeviceDisconnected(a)
	sip.expectEvent(t, "BYE")
	waitClosed(t, done, "HandleIncoming")
}

func TestOutgoingCallWithEarlyMediaAndRemoteHangup(t *testing.T) {
	h := newHarness(t)
	out := newFakeSIPOut()
	media := newFakeMedia()
	answerGate := make(chan struct{})
	h.sip.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
		ev.OnRinging()
		ev.OnEarlyMedia(media)
		<-answerGate
		return out, media, nil
	}
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	callID := uuid.NewString()
	h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: strings.ToUpper(callID), Number: "+4930123456"})

	var offer protocol.CallOffer
	a.expect(t, protocol.TypeCallOffer, &offer)
	if offer.CallID != callID {
		t.Fatalf("call id not normalized: %s", offer.CallID)
	}
	if !strings.Contains(offer.SDP, "[G722 PCMA PCMU]") {
		t.Fatalf("outgoing offer should contain all codecs: %s", offer.SDP)
	}
	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: callID, SDP: "codec:PCMA"})
	select {
	case d := <-h.sip.dials:
		if d != "+4930123456/PCMA" {
			t.Fatalf("dialed %q", d)
		}
	case <-time.After(waitTimeout):
		t.Fatal("no dial")
	}
	var st protocol.CallState
	a.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateRinging {
		t.Fatalf("state %q", st.State)
	}
	a.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateEarlyMedia {
		t.Fatalf("state %q", st.State)
	}
	// Early media already flows.
	peer := h.peers.peer(t, 0)
	media.in <- rtpPacket(8, 1)
	select {
	case <-peer.toDev:
	case <-time.After(waitTimeout):
		t.Fatal("early media not relayed")
	}
	close(answerGate)
	a.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateConnected {
		t.Fatalf("state %q", st.State)
	}
	out.remoteEnd()
	a.expectEnded(t, protocol.EndReasonRemoteHangup)
}

func TestOutgoingFailures(t *testing.T) {
	for _, tc := range []struct {
		status       int
		reason       string
		invalidError bool
	}{
		{486, protocol.EndReasonBusy, false},
		{600, protocol.EndReasonBusy, false},
		{603, protocol.EndReasonRejected, false},
		{404, protocol.EndReasonFailed, true},
		{484, protocol.EndReasonFailed, true},
		{500, protocol.EndReasonFailed, false},
	} {
		t.Run(tc.reason+"-"+string(rune('0'+tc.status/100)), func(t *testing.T) {
			h := newHarness(t)
			h.sip.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
				return nil, nil, &DialError{Status: tc.status, Reason: "x"}
			}
			a := newFakeConn("dev-a")
			callID := uuid.NewString()
			h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "123"})
			a.expect(t, protocol.TypeCallOffer, nil)
			h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: callID, SDP: "codec:G722"})
			if tc.invalidError {
				var e protocol.Error
				a.expect(t, protocol.TypeError, &e)
				if e.Code != protocol.ErrorInvalidNumber {
					t.Fatalf("error %q", e.Code)
				}
			}
			ended := a.expectEnded(t, tc.reason)
			if ended.SIPCode != tc.status {
				t.Fatalf("sipCode %d", ended.SIPCode)
			}
		})
	}
}

func TestOutgoingHangupWhileDialingCancels(t *testing.T) {
	h := newHarness(t)
	cancelled := make(chan struct{})
	h.sip.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, nil, ctx.Err()
	}
	a := newFakeConn("dev-a")
	callID := uuid.NewString()
	h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "123"})
	a.expect(t, protocol.TypeCallOffer, nil)
	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: callID, SDP: "x"})
	<-h.sip.dials
	h.send(a, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID})
	a.expectEnded(t, protocol.EndReasonLocalHangup)
	waitClosed(t, cancelled, "dial cancel")
	a.expectNothing(t)
}

func TestOutgoingRequiresRegistrationAndValidInput(t *testing.T) {
	h := newHarness(t)
	a := newFakeConn("dev-a")

	h.sip.registered = false
	h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: uuid.NewString(), Number: "123"})
	var e protocol.Error
	a.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorSIPUnavailable {
		t.Fatalf("code %q", e.Code)
	}
	a.expectEnded(t, protocol.EndReasonFailed)

	h.sip.registered = true
	h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: uuid.NewString(), Number: "12a3"})
	a.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorInvalidNumber {
		t.Fatalf("code %q", e.Code)
	}
	a.expectEnded(t, protocol.EndReasonFailed)

	h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: "nope", Number: "123"})
	a.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorBadRequest {
		t.Fatalf("code %q", e.Code)
	}

	unknown := uuid.NewString()
	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: unknown, SDP: "x"})
	a.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorCallNotFound {
		t.Fatalf("code %q", e.Code)
	}
	h.send(a, protocol.TypeCallHangup, protocol.CallHangup{CallID: unknown})
	a.expectEnded(t, protocol.EndReasonNotFound)
}

func TestOutgoingReattachTimeoutCancelsDial(t *testing.T) {
	h := newHarness(t)
	h.sip.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	callID := uuid.NewString()
	h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "123"})
	a.expect(t, protocol.TypeCallOffer, nil)
	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: callID, SDP: "x"})
	<-h.sip.dials
	h.m.DeviceDisconnected(a)
	deadline := time.Now().Add(waitTimeout)
	for h.m.ActiveCalls() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("call not ended after re-attach timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestShutdownEndsCalls(t *testing.T) {
	h := newHarness(t, device("dev-a", "tok"))
	sip := newFakeSIPIn("123", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	h.expectPush(t)
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	h.m.Shutdown(ctx)
	sip.expectEvent(t, "503")
	waitClosed(t, done, "HandleIncoming")
}

func TestIncomingDoubleAttachReusesPeerAndResendsPendingOffer(t *testing.T) {
	h := newHarness(t, device("dev-a", ""))
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	sip := newFakeSIPIn("123", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	var inc protocol.CallIncoming
	a.expect(t, protocol.TypeCallIncoming, &inc) // unsolicited notification, no offer yet
	a.expectNothing(t)

	h.send(a, protocol.TypeCallAttach, protocol.CallAttach{CallID: inc.CallID})
	a.expect(t, protocol.TypeCallIncoming, nil)
	var first protocol.CallOffer
	a.expect(t, protocol.TypeCallOffer, &first)

	// Same connection attaches again before answering: same offer again.
	h.send(a, protocol.TypeCallAttach, protocol.CallAttach{CallID: inc.CallID})
	a.expect(t, protocol.TypeCallIncoming, nil)
	var second protocol.CallOffer
	a.expect(t, protocol.TypeCallOffer, &second)
	if second.SDP != first.SDP {
		t.Fatalf("pending offer must be resent unchanged:\n%s\n%s", first.SDP, second.SDP)
	}
	// Both answers arrive; the duplicate is ignored without error.
	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: inc.CallID, SDP: "x"})
	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: inc.CallID, SDP: "x"})
	a.expectNothing(t)

	// Attach once more after answering: ICE restart on the same peer.
	h.send(a, protocol.TypeCallAttach, protocol.CallAttach{CallID: inc.CallID})
	a.expect(t, protocol.TypeCallIncoming, nil)
	var third protocol.CallOffer
	a.expect(t, protocol.TypeCallOffer, &third)
	if !strings.Contains(third.SDP, "restart=true") {
		t.Fatalf("expected ICE restart offer, got %s", third.SDP)
	}
	h.peers.mu.Lock()
	peers := len(h.peers.peers)
	h.peers.mu.Unlock()
	if peers != 1 {
		t.Fatalf("repeated attach created %d PeerConnections", peers)
	}

	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: inc.CallID, SDP: "y"})
	h.send(a, protocol.TypeCallAccept, protocol.CallAccept{CallID: inc.CallID})
	sip.expectEvent(t, "200")
	a.expect(t, protocol.TypeCallState, nil)
	sip.remoteEnd(SIPEndRemoteHangup)
	a.expectEnded(t, protocol.EndReasonRemoteHangup)
	waitClosed(t, done, "HandleIncoming")
}

func TestIncomingRingingReconnectKeepsPeer(t *testing.T) {
	h := newHarness(t, device("dev-a", "tok"))
	sip := newFakeSIPIn("123", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	push := h.expectPush(t)

	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	h.attachAndAnswer(t, a, push.CallID, "x")

	// The WebSocket drops while ringing and comes back.
	h.m.DeviceDisconnected(a)
	a2 := newFakeConn("dev-a")
	h.m.DeviceConnected(a2)
	h.send(a2, protocol.TypeCallAttach, protocol.CallAttach{CallID: push.CallID})
	a2.expect(t, protocol.TypeCallIncoming, nil)
	var offer protocol.CallOffer
	a2.expect(t, protocol.TypeCallOffer, &offer)
	if !strings.Contains(offer.SDP, "restart=true") {
		t.Fatalf("expected ICE restart on the existing peer: %s", offer.SDP)
	}
	if h.peers.peer(t, 0).isClosed() {
		t.Fatal("peer was closed on disconnect")
	}
	sip.remoteEnd(SIPEndCancelled)
	a2.expectEnded(t, protocol.EndReasonRemoteCancelled)
	waitClosed(t, done, "HandleIncoming")
}

func TestOutgoingReattachBeforeAnswerResendsOffer(t *testing.T) {
	h := newHarness(t)
	h.sip.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	callID := uuid.NewString()
	h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "123"})
	var first protocol.CallOffer
	a.expect(t, protocol.TypeCallOffer, &first)
	h.send(a, protocol.TypeCallAttach, protocol.CallAttach{CallID: callID})
	var again protocol.CallOffer
	a.expect(t, protocol.TypeCallOffer, &again)
	if again.SDP != first.SDP {
		t.Fatal("unanswered offer must be resent unchanged")
	}
	h.send(a, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID})
	a.expectEnded(t, protocol.EndReasonLocalHangup)
}
