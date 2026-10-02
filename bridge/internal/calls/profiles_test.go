package calls

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// ADR-0008: profile B's line only rings B's devices, and a device of the
// default profile cannot reach B's calls by their ID.

func profileDevice(id, token, profileID string) store.Device {
	d := device(id, token)
	d.Profile = profileID
	return d
}

func newProfileHarness(t *testing.T) (*harness, *fakeSIPLeg) {
	t.Helper()
	h := newHarness(t, device("dev-a", "tok-a"), profileDevice("dev-b", "tok-b", "b"))
	lineB := &fakeSIPLeg{registered: true, dials: make(chan string, 5)}
	h.m.SetLine("b", lineB)
	return h, lineB
}

func TestIncomingOnLineRingsOnlyItsProfile(t *testing.T) {
	h, _ := newProfileHarness(t)
	a, b := newFakeConn("dev-a"), newFakeConn("dev-b")
	b.profile = "b"
	h.m.DeviceConnected(a)
	h.m.DeviceConnected(b)

	sip := newFakeSIPIn("0301234567", codec.PCMA)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.m.HandleIncomingOn(context.Background(), "b", sip)
	}()

	push := h.expectPush(t)
	var incoming protocol.CallIncoming
	b.expect(t, protocol.TypeCallIncoming, &incoming)
	if push.CallID != incoming.CallID {
		t.Fatalf("push for %s, incoming %s", push.CallID, incoming.CallID)
	}
	select {
	case p := <-h.pusher.pushes:
		t.Fatalf("second push %+v: only profile b's device may be woken", p)
	case <-time.After(50 * time.Millisecond):
	}
	a.expectNothing(t)

	// dev-a knows the call ID (e.g. guessed or leaked): it still cannot
	// attach, accept or ask for the call's state.
	h.send(a, protocol.TypeCallAttach, protocol.CallAttach{CallID: incoming.CallID})
	a.expectEnded(t, protocol.EndReasonNotFound)
	h.send(a, protocol.TypeCallAccept, protocol.CallAccept{CallID: incoming.CallID})
	a.expectEnded(t, protocol.EndReasonNotFound)
	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: incoming.CallID, SDP: "codec:PCMA"})
	var e protocol.Error
	a.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorCallNotFound {
		t.Fatalf("answer from another profile: %+v", e)
	}
	h.expectNoStatus(t, "dev-a", incoming.CallID)

	// After the call ended its tombstone stays hidden from dev-a too.
	sip.remoteEnd(SIPEndCancelled)
	waitClosed(t, done, "HandleIncomingOn")
	b.expectEnded(t, protocol.EndReasonRemoteCancelled)
	h.send(a, protocol.TypeCallAttach, protocol.CallAttach{CallID: incoming.CallID})
	a.expectEnded(t, protocol.EndReasonNotFound)
}

func TestIncomingOnDefaultLineSkipsOtherProfiles(t *testing.T) {
	h, _ := newProfileHarness(t)
	b := newFakeConn("dev-b")
	b.profile = "b"
	h.m.DeviceConnected(b)

	sip := newFakeSIPIn("0301234567", codec.PCMA)
	done := h.incoming(sip)
	push := h.expectPush(t)
	if push.CallID == "" {
		t.Fatal("default profile's device not pushed")
	}
	b.expectNothing(t)
	sip.remoteEnd(SIPEndCancelled)
	waitClosed(t, done, "HandleIncoming")
}

func TestOutgoingUsesOwnLine(t *testing.T) {
	h, lineB := newProfileHarness(t)
	lineB.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	b := newFakeConn("dev-b")
	b.profile = "b"
	h.m.DeviceConnected(b)

	id := uuid.NewString()
	h.send(b, protocol.TypeCallDial, protocol.CallDial{CallID: id, Number: "0301234569"})
	b.expect(t, protocol.TypeCallOffer, nil)
	h.send(b, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: id, SDP: "codec:PCMA"})
	select {
	case d := <-lineB.dials:
		if d != "0301234569/PCMA" {
			t.Fatalf("dialed %q", d)
		}
	case <-time.After(waitTimeout):
		t.Fatal("profile b did not dial over its own line")
	}
	select {
	case d := <-h.sip.dials:
		t.Fatalf("default line dialed %q for profile b", d)
	default:
	}
	h.send(b, protocol.TypeCallHangup, protocol.CallHangup{CallID: id})
	b.expect(t, protocol.TypeCallEnded, nil)
}

func TestOutgoingFailsWhenOwnLineIsDown(t *testing.T) {
	h, lineB := newProfileHarness(t)
	lineB.registered = false
	b := newFakeConn("dev-b")
	b.profile = "b"
	h.m.DeviceConnected(b)

	h.send(b, protocol.TypeCallDial, protocol.CallDial{CallID: uuid.NewString(), Number: "0301234569"})
	var e protocol.Error
	b.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorSIPUnavailable {
		t.Fatalf("dial on an unregistered line: %+v", e)
	}
	b.expectEnded(t, protocol.EndReasonFailed)
	if !h.m.SIPRegisteredFor("") || h.m.SIPRegisteredFor("b") || h.m.SIPRegistered() {
		t.Fatal("registration per profile wrong")
	}

	// A device whose profile has no line (removed from the config) can't
	// dial at all.
	ghost := newFakeConn("dev-x")
	ghost.profile = "gone"
	h.m.DeviceConnected(ghost)
	h.send(ghost, protocol.TypeCallDial, protocol.CallDial{CallID: uuid.NewString(), Number: "0301234569"})
	ghost.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorSIPUnavailable {
		t.Fatalf("dial without a line: %+v", e)
	}
}

func TestStatusGoesToItsProfileOnly(t *testing.T) {
	h, _ := newProfileHarness(t)
	a, b := newFakeConn("dev-a"), newFakeConn("dev-b")
	b.profile = "b"
	h.m.DeviceConnected(a)
	h.m.DeviceConnected(b)

	h.m.BroadcastProfileStatus("b", false)
	var st protocol.Status
	b.expect(t, protocol.TypeStatus, &st)
	if st.SIPRegistered {
		t.Fatalf("status = %+v", st)
	}
	a.expectNothing(t)

	h.m.BroadcastStatus(true)
	a.expect(t, protocol.TypeStatus, nil)
	b.expectNothing(t)
}
