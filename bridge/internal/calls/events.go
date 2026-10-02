package calls

import "time"

// EventKind names what happened to a call (for statistics and the admin
// TUI). Numbers and names are passed raw; consumers mask them.
type EventKind string

const (
	EventStarted     EventKind = "started"
	EventAccepted    EventKind = "accepted"
	EventConnected   EventKind = "connected"
	EventEnded       EventKind = "ended"
	EventPushOK      EventKind = "push_ok"
	EventPushFailed  EventKind = "push_failed"
	EventMediaFailed EventKind = "media_failed"
)

// Event is one call event. Fields not relevant for a kind are empty.
type Event struct {
	Kind      EventKind
	At        time.Time
	CallID    string
	Profile   string // household profile of the line (ADR-0008)
	Direction string // "incoming" | "outgoing"
	Number    string // remote party
	Name      string // remote party name, if known
	DeviceID  string
	Codec     string
	Reason    string
	SIPCode   int
	Error     string
}

func (m *Manager) emit(e Event) {
	if m.opts.OnEvent == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	m.opts.OnEvent(e)
}

func (c *call) event(kind EventKind) Event {
	number, name := c.caller, c.callerName
	if c.dir == directionOutgoing {
		number, name = c.number, ""
	}
	return Event{Kind: kind, CallID: c.id, Profile: c.profile, Direction: c.dir.String(), Number: number, Name: name, Codec: string(c.codec)}
}
