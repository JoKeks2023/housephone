package admin

import (
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
)

func TestStatsPerProfile(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.Local)
	r := NewRecorder(10, func() time.Time { return at })
	r.Handle(calls.Event{Kind: calls.EventStarted, At: at, CallID: "1", Profile: "b", Direction: "incoming"})
	r.Handle(calls.Event{Kind: calls.EventEnded, At: at, CallID: "1"})
	r.Handle(calls.Event{Kind: calls.EventStarted, At: at, CallID: "2", Profile: "default", Direction: "outgoing"})

	s := r.Stats()
	if b := s.ByProfile["b"]; b.Incoming != 1 || b.Missed != 1 || b.Outgoing != 0 {
		t.Fatalf("profile b: %+v", b)
	}
	if a := s.ByProfile["default"]; a.Outgoing != 1 || a.Incoming != 0 {
		t.Fatalf("default profile: %+v", a)
	}
	if s.Total.Incoming != 1 || s.Total.Outgoing != 1 {
		t.Fatalf("total: %+v", s.Total)
	}
	_, recent := r.Calls()
	if len(recent) != 1 || recent[0].Profile != "b" {
		t.Fatalf("recent: %+v", recent)
	}
}
