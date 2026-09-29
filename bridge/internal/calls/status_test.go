package calls

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// Tests for GET /v1/calls/{callId} (CallStatus): a ringing watch may not
// open a WebSocket, so it polls the call from its own point of view.

func (h *harness) expectStatus(t *testing.T, deviceID, callID string, want protocol.CallStatus) {
	t.Helper()
	got, ok := h.m.CallStatus(deviceID, callID)
	if !ok {
		t.Fatalf("status for %s: not found, want %+v", deviceID, want)
	}
	if got != want {
		t.Fatalf("status for %s: %+v, want %+v", deviceID, got, want)
	}
}

func (h *harness) expectNoStatus(t *testing.T, deviceID, callID string) {
	t.Helper()
	if got, ok := h.m.CallStatus(deviceID, callID); ok {
		t.Fatalf("status for %s: %+v, want not found", deviceID, got)
	}
}

func ringing(callID string) protocol.CallStatus {
	return protocol.CallStatus{CallID: callID, State: protocol.CallStatusRinging}
}

func endedStatus(callID, reason string, sipCode int) protocol.CallStatus {
	return protocol.CallStatus{CallID: callID, State: protocol.CallStatusEnded, Reason: reason, SIPCode: sipCode}
}

func TestCallStatusFollowsTheCallPerDevice(t *testing.T) {
	h := newHarness(t, device("phone", "tok-p"), watchDevice("watch", "tok-w"))
	sip := newFakeSIPIn("+4930123456", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	callID := h.expectPush(t).CallID
	h.expectPush(t)

	// Both ring; the watch never attaches (no WebSocket while ringing).
	h.expectStatus(t, "watch", callID, ringing(callID))
	h.expectStatus(t, "phone", callID, ringing(callID))
	h.expectNoStatus(t, "stranger", callID)
	h.expectNoStatus(t, "watch", uuid.NewString())
	h.expectNoStatus(t, "watch", "not-a-uuid")

	phone := newFakeConn("phone")
	h.m.DeviceConnected(phone)
	h.attachAndAnswer(t, phone, callID, "answer")
	h.send(phone, protocol.TypeCallAccept, protocol.CallAccept{CallID: callID})
	sip.expectEvent(t, "200")
	phone.expect(t, protocol.TypeCallState, nil)

	h.expectStatus(t, "watch", callID, endedStatus(callID, protocol.EndReasonAnsweredElsewhere, 0))
	h.expectStatus(t, "phone", callID, protocol.CallStatus{CallID: callID, State: protocol.CallStatusConnected})

	sip.remoteEnd(SIPEndRemoteHangup)
	phone.expectEnded(t, protocol.EndReasonRemoteHangup)
	waitClosed(t, done, "HandleIncoming")

	// After the end the tombstone keeps each device's own view.
	h.expectStatus(t, "watch", callID, endedStatus(callID, protocol.EndReasonAnsweredElsewhere, 0))
	h.expectStatus(t, "phone", callID, endedStatus(callID, protocol.EndReasonRemoteHangup, 0))
	h.expectNoStatus(t, "stranger", callID)
}

func TestCallStatusTellsAnUnattachedWatchAboutCancel(t *testing.T) {
	h := newHarness(t, watchDevice("watch", "tok-w"))
	sip := newFakeSIPIn("+4930123456", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	callID := h.expectPush(t).CallID
	h.expectStatus(t, "watch", callID, ringing(callID))

	sip.remoteEnd(SIPEndCancelled)
	waitClosed(t, done, "HandleIncoming")
	h.expectStatus(t, "watch", callID, endedStatus(callID, protocol.EndReasonRemoteCancelled, 0))
}

func TestCallStatusKeepsTheOwnDecline(t *testing.T) {
	h := newHarness(t, device("phone", "tok-p"), watchDevice("watch", "tok-w"))
	sip := newFakeSIPIn("+4930123456", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	callID := h.expectPush(t).CallID
	h.expectPush(t)

	phone := newFakeConn("phone")
	h.m.DeviceConnected(phone)
	h.attachAndAnswer(t, phone, callID, "answer")
	h.send(phone, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID, Reason: protocol.HangupReasonDeclined})
	phone.expectEnded(t, protocol.EndReasonLocalHangup)

	h.expectStatus(t, "phone", callID, endedStatus(callID, protocol.EndReasonLocalHangup, 0))
	h.expectStatus(t, "watch", callID, ringing(callID))

	sip.remoteEnd(SIPEndCancelled)
	waitClosed(t, done, "HandleIncoming")
	h.expectStatus(t, "phone", callID, endedStatus(callID, protocol.EndReasonLocalHangup, 0))
	h.expectStatus(t, "watch", callID, endedStatus(callID, protocol.EndReasonRemoteCancelled, 0))
}

func TestCallStatusForOutgoingCalls(t *testing.T) {
	h := newHarness(t, device("phone", ""), watchDevice("watch", ""))
	phone := newFakeConn("phone")
	h.m.DeviceConnected(phone)
	callID := uuid.NewString()
	h.send(phone, protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "+4930123456"})

	h.expectStatus(t, "phone", callID, ringing(callID))
	h.expectNoStatus(t, "watch", callID)
	phone.expect(t, protocol.TypeCallOffer, nil)

	h.send(phone, protocol.TypeCallHangup, protocol.CallHangup{CallID: callID, Reason: protocol.HangupReasonHangup})
	phone.expectEnded(t, protocol.EndReasonLocalHangup)
	eventually(t, "call removed", func() bool { return h.m.ActiveCalls() == 0 })
	h.expectStatus(t, "phone", callID, endedStatus(callID, protocol.EndReasonLocalHangup, 0))
}

func TestCallStatusForgetsExpiredTombstones(t *testing.T) {
	h := newHarness(t, watchDevice("watch", "tok-w"))
	h.m.opts.TombstoneTTL = 20 * time.Millisecond
	sip := newFakeSIPIn("+4930123456", codec.G722)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	callID := h.expectPush(t).CallID
	sip.remoteEnd(SIPEndCancelled)
	waitClosed(t, done, "HandleIncoming")
	h.expectStatus(t, "watch", callID, endedStatus(callID, protocol.EndReasonRemoteCancelled, 0))

	time.Sleep(40 * time.Millisecond)
	h.expectNoStatus(t, "watch", callID)
}
