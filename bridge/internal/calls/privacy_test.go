package calls

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Neither the caller's number and name nor a dialed number appear in the log
// in full (log.showNumbers is off by default).
func TestCallLogsMaskNumbersAndNames(t *testing.T) {
	var logs lockedBuffer
	h := newHarness(t, device("dev-a", ""))
	h.m = NewManager(Options{
		BridgeID: "bridge-1", SIP: h.sip, Peers: h.peers, Pusher: h.pusher, Devices: h.dir,
		Logger:          slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		ReattachTimeout: 150 * time.Millisecond, AudioFrameInterval: 5 * time.Millisecond,
	})
	a := newFakeConn("dev-a")
	h.m.DeviceConnected(a)

	sip := newFakeSIPIn("0301234567", codec.PCMA) // caller name "Oma"
	done := h.incoming(sip)
	sip.expectEvent(t, "180")
	var inc protocol.CallIncoming
	a.expect(t, protocol.TypeCallIncoming, &inc)
	h.send(a, protocol.TypeCallHangup, protocol.CallHangup{CallID: inc.CallID, Reason: protocol.HangupReasonDeclined})
	a.expectEnded(t, protocol.EndReasonLocalHangup)
	waitClosed(t, done, "HandleIncoming")

	h.send(a, protocol.TypeCallDial, protocol.CallDial{CallID: "3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93", Number: "0307654321"})
	a.expect(t, protocol.TypeCallOffer, nil)

	out := logs.String()
	for _, secret := range []string{"0301234567", "Oma", "0307654321"} {
		if strings.Contains(out, secret) {
			t.Errorf("log contains %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "…567") || !strings.Contains(out, "…321") || !strings.Contains(out, "hasCallerName=true") {
		t.Fatalf("masked values missing:\n%s", out)
	}
}
