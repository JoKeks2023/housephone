package calls

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// newLimitedHarness is newHarness with custom limits.
func newLimitedHarness(t *testing.T, maxCalls int, offerAnswer time.Duration) *harness {
	t.Helper()
	h := newHarness(t)
	h.m = NewManager(Options{
		BridgeID:           "bridge-1",
		SIP:                h.sip,
		Peers:              h.peers,
		Pusher:             h.pusher,
		Devices:            h.dir,
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		ReattachTimeout:    150 * time.Millisecond,
		AudioFrameInterval: 5 * time.Millisecond,
		MaxCalls:           maxCalls,
		OfferAnswerTimeout: offerAnswer,
	})
	return h
}

func (h *harness) dialUnanswered(t *testing.T, conn *fakeConn) string {
	t.Helper()
	id := uuid.NewString()
	h.send(conn, protocol.TypeCallDial, protocol.CallDial{CallID: id, Number: "0301234567"})
	return id
}

// Security review N3: a device cannot open calls without end.
func TestDialLimitPerDevice(t *testing.T) {
	h := newLimitedHarness(t, 8, time.Minute)
	a, b := newFakeConn("dev-a"), newFakeConn("dev-b")
	h.m.DeviceConnected(a)
	h.m.DeviceConnected(b)

	for range MaxOutgoingPerDevice {
		h.dialUnanswered(t, a)
		a.expect(t, protocol.TypeCallOffer, nil)
	}
	h.dialUnanswered(t, a)
	var e protocol.Error
	a.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorTooManyCalls {
		t.Fatalf("third dial: %+v", e)
	}
	a.expectEnded(t, protocol.EndReasonFailed)

	// Another device still may dial.
	h.dialUnanswered(t, b)
	b.expect(t, protocol.TypeCallOffer, nil)
}

func TestDialLimitGlobal(t *testing.T) {
	h := newLimitedHarness(t, 2, time.Minute)
	a, b, c := newFakeConn("dev-a"), newFakeConn("dev-b"), newFakeConn("dev-c")
	for _, conn := range []*fakeConn{a, b, c} {
		h.m.DeviceConnected(conn)
	}
	h.dialUnanswered(t, a)
	a.expect(t, protocol.TypeCallOffer, nil)
	h.dialUnanswered(t, b)
	b.expect(t, protocol.TypeCallOffer, nil)
	h.dialUnanswered(t, c)
	var e protocol.Error
	c.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorTooManyCalls {
		t.Fatalf("dial beyond bridge limit: %+v", e)
	}
}

func TestUnansweredOfferEndsOutgoingCall(t *testing.T) {
	h := newLimitedHarness(t, 8, 100*time.Millisecond)
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	h.dialUnanswered(t, a)
	a.expect(t, protocol.TypeCallOffer, nil)
	a.expectEnded(t, protocol.EndReasonFailed)
	deadline := time.Now().Add(waitTimeout)
	for h.m.ActiveCalls() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := h.m.ActiveCalls(); n != 0 {
		t.Fatalf("%d calls still active", n)
	}
}

func TestDTMFQueueIsBounded(t *testing.T) {
	h := newLimitedHarness(t, 8, time.Minute)
	out := newFakeSIPOut()
	media := newFakeMedia()
	h.sip.dial = func(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error) {
		return out, media, nil
	}
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)
	id := h.dialUnanswered(t, a)
	a.expect(t, protocol.TypeCallOffer, nil)
	h.send(a, protocol.TypeCallAnswer, protocol.CallAnswer{CallID: id, SDP: "codec:PCMA"})
	var st protocol.CallState
	a.expect(t, protocol.TypeCallState, &st)
	if st.State != protocol.CallStateConnected {
		t.Fatalf("state %q", st.State)
	}

	// Nobody drains media.dtmf (buffer 10): the worker blocks, the queue
	// fills, and further digits are refused instead of piling up.
	for range 30 {
		h.send(a, protocol.TypeCallDTMF, protocol.CallDTMF{CallID: id, Digits: "1"})
	}
	var e protocol.Error
	a.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.ErrorBadRequest {
		t.Fatalf("error %+v", e)
	}
	out.remoteEnd()
}
