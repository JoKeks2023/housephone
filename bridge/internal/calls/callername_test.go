package calls

import (
	"sync/atomic"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// The FRITZ!Box phonebook fills in the caller name when the INVITE has no
// display name (v1.2); an existing display name wins, and an ambiguous or
// unknown number stays without name.
func TestCallerNameFromPhonebook(t *testing.T) {
	phonebook := map[string]string{"030123456": "Oma", "0301111": ""}
	for _, tc := range []struct {
		name        string
		caller      string
		displayName string
		want        string
		lookups     int32
	}{
		{"display name missing, number known", "030123456", "", "Oma", 1},
		{"display name wins", "030123456", "Opa (FRITZ!Box)", "Opa (FRITZ!Box)", 0},
		{"ambiguous number", "0301111", "", "", 1},
		{"unknown number", "0309999", "", "", 1},
		{"withheld number", "", "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, device("dev-a", "tok-a"))
			var lookups atomic.Int32
			h.m.opts.CallerNames = func(_, number string) string {
				lookups.Add(1)
				return phonebook[number]
			}
			sip := newFakeSIPIn(tc.caller, codec.G722)
			sip.callerName = tc.displayName
			done := h.incoming(sip)

			push := h.expectPush(t)
			if push.CallerName != tc.want {
				t.Fatalf("push callerName %q, want %q", push.CallerName, tc.want)
			}
			conn := newFakeConn("dev-a")
			h.m.DeviceConnected(conn)
			h.send(conn, protocol.TypeCallAttach, protocol.CallAttach{CallID: push.CallID})
			var incoming protocol.CallIncoming
			conn.expect(t, protocol.TypeCallIncoming, &incoming)
			if incoming.CallerName != tc.want {
				t.Fatalf("call.incoming callerName %q, want %q", incoming.CallerName, tc.want)
			}
			if got := lookups.Load(); got != tc.lookups {
				t.Fatalf("phonebook lookups = %d, want %d", got, tc.lookups)
			}
			sip.remoteEnd(SIPEndCancelled)
			waitClosed(t, done, "HandleIncoming")
		})
	}
}
