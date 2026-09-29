package calls

import (
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// A device removed while connected (security review M1) drops out of a
// ringing call without ending it for the others, and a removed active device
// hangs up the connected call.
func TestDeviceRevokedDuringCall(t *testing.T) {
	h := newHarness(t, device("dev-a", ""), device("dev-b", ""))
	a, b, other := newFakeConn("dev-a"), newFakeConn("dev-b"), newFakeConn("dev-x")
	h.m.DeviceConnected(a)
	h.m.DeviceConnected(b)

	sip := newFakeSIPIn("0301234", codec.PCMA)
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	var inc protocol.CallIncoming
	a.expect(t, protocol.TypeCallIncoming, &inc)
	b.expect(t, protocol.TypeCallIncoming, nil)
	h.attachAndAnswer(t, a, inc.CallID, "answer-a")

	// A device that is not part of the call changes nothing.
	h.m.DeviceRevoked(other)
	other.expectNothing(t)

	h.m.DeviceRevoked(a)
	a.expectEnded(t, protocol.EndReasonLocalHangup)
	sip.mu.Lock()
	rejected := sip.rejected
	sip.mu.Unlock()
	if rejected != 0 {
		t.Fatalf("call rejected (%d) although dev-b can still answer", rejected)
	}

	h.attachAndAnswer(t, b, inc.CallID, "answer-b")
	h.send(b, protocol.TypeCallAccept, protocol.CallAccept{CallID: inc.CallID})
	sip.expectEvent(t, "200")
	b.expect(t, protocol.TypeCallState, nil)

	h.m.DeviceRevoked(b)
	b.expectEnded(t, protocol.EndReasonLocalHangup)
	sip.expectEvent(t, "BYE")
	waitClosed(t, done, "HandleIncoming")
}
