package admin

import (
	"sync"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/logsafe"
)

// Counters are call statistics for one period.
type Counters struct {
	Incoming      int            `json:"incoming"`
	Answered      int            `json:"answered"`
	Missed        int            `json:"missed"`
	Outgoing      int            `json:"outgoing"`
	PushFailures  int            `json:"pushFailures"`
	MediaFailures int            `json:"mediaFailures"`
	Codecs        map[string]int `json:"codecs"`
}

func newCounters() Counters { return Counters{Codecs: map[string]int{}} }

// Stats are the counters since the bridge started and for today.
type Stats struct {
	Since    time.Time `json:"since"`
	Total    Counters  `json:"total"`
	Today    Counters  `json:"today"`
	LastPush *PushInfo `json:"lastPush,omitempty"`
}

// PushInfo is the result of the latest VoIP push.
type PushInfo struct {
	At       time.Time `json:"at"`
	DeviceID string    `json:"deviceId"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
}

// CallInfo is one call for the TUI. Number and name are masked unless
// log.showNumbers is on.
type CallInfo struct {
	ID          string    `json:"id"`
	Direction   string    `json:"direction"`
	Number      string    `json:"number"`
	Name        string    `json:"name,omitempty"`
	HasName     bool      `json:"hasName"`
	DeviceID    string    `json:"deviceId,omitempty"`
	Codec       string    `json:"codec,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	ConnectedAt time.Time `json:"connectedAt,omitzero"`
	EndedAt     time.Time `json:"endedAt,omitzero"`
	Reason      string    `json:"reason,omitempty"`
	SIPCode     int       `json:"sipCode,omitempty"`
}

// Recorder turns call events into statistics and the call list.
type Recorder struct {
	mu      sync.Mutex
	now     func() time.Time
	since   time.Time
	day     string
	total   Counters
	today   Counters
	active  map[string]*CallInfo
	recent  []CallInfo // newest first
	keep    int
	push    *PushInfo
	counted map[string]bool // calls already counted as answered
}

// NewRecorder keeps the last keep finished calls.
func NewRecorder(keep int, now func() time.Time) *Recorder {
	if now == nil {
		now = time.Now
	}
	r := &Recorder{now: now, keep: keep, active: map[string]*CallInfo{}, counted: map[string]bool{}, total: newCounters(), today: newCounters()}
	r.since = now()
	r.day = r.since.Local().Format("2006-01-02")
	return r
}

// Handle is a calls.Options.OnEvent callback.
func (r *Recorder) Handle(e calls.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollDay(e.At)
	both := func(f func(c *Counters)) { f(&r.total); f(&r.today) }
	switch e.Kind {
	case calls.EventStarted:
		ci := &CallInfo{ID: e.CallID, Direction: e.Direction, Number: logsafe.Number(e.Number), HasName: e.Name != "", DeviceID: e.DeviceID, Codec: e.Codec, StartedAt: e.At}
		if logsafe.ShowNumbers() {
			ci.Name = e.Name
		}
		r.active[e.CallID] = ci
		if e.Direction == "outgoing" {
			both(func(c *Counters) { c.Outgoing++ })
		} else {
			both(func(c *Counters) { c.Incoming++ })
		}
	case calls.EventAccepted:
		if ci := r.active[e.CallID]; ci != nil {
			ci.DeviceID, ci.Codec = e.DeviceID, e.Codec
		}
	case calls.EventConnected:
		if ci := r.active[e.CallID]; ci != nil {
			ci.ConnectedAt, ci.Codec = e.At, e.Codec
			if !r.counted[e.CallID] {
				r.counted[e.CallID] = true
				if ci.Direction == "incoming" {
					both(func(c *Counters) { c.Answered++ })
				}
				both(func(c *Counters) { c.Codecs[e.Codec]++ })
			}
		}
	case calls.EventEnded:
		ci := r.active[e.CallID]
		if ci == nil {
			return
		}
		delete(r.active, e.CallID)
		ci.EndedAt, ci.Reason, ci.SIPCode = e.At, e.Reason, e.SIPCode
		if e.DeviceID != "" {
			ci.DeviceID = e.DeviceID
		}
		if ci.Direction == "incoming" && ci.ConnectedAt.IsZero() {
			both(func(c *Counters) { c.Missed++ })
		}
		delete(r.counted, e.CallID)
		r.recent = append([]CallInfo{*ci}, r.recent...)
		if len(r.recent) > r.keep {
			r.recent = r.recent[:r.keep]
		}
	case calls.EventPushOK, calls.EventPushFailed:
		ok := e.Kind == calls.EventPushOK
		r.push = &PushInfo{At: e.At, DeviceID: e.DeviceID, OK: ok, Error: e.Error}
		if !ok {
			both(func(c *Counters) { c.PushFailures++ })
		}
	case calls.EventMediaFailed:
		both(func(c *Counters) { c.MediaFailures++ })
	}
}

func (r *Recorder) rollDay(at time.Time) {
	if at.IsZero() {
		at = r.now()
	}
	day := at.Local().Format("2006-01-02")
	if day != r.day {
		r.day = day
		r.today = newCounters()
	}
}

// Stats returns a copy of the counters.
func (r *Recorder) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollDay(time.Time{})
	s := Stats{Since: r.since, Total: copyCounters(r.total), Today: copyCounters(r.today)}
	if r.push != nil {
		p := *r.push
		s.LastPush = &p
	}
	return s
}

// Calls returns the active calls and the recent ones (newest first).
func (r *Recorder) Calls() (active, recent []CallInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.active {
		active = append(active, *c)
	}
	recent = append(recent, r.recent...)
	return active, recent
}

func copyCounters(c Counters) Counters {
	out := c
	out.Codecs = make(map[string]int, len(c.Codecs))
	for k, v := range c.Codecs {
		out.Codecs[k] = v
	}
	return out
}
