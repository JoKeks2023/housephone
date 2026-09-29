package admin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// LogLine is one formatted log record kept for the admin TUI.
type LogLine struct {
	Seq   uint64    `json:"seq"`
	At    time.Time `json:"at"`
	Level string    `json:"level"`
	Text  string    `json:"text"` // message plus key=value attributes
}

// LogRing keeps the last N log lines. Its Handler tees records into it
// while passing them on to the real handler. The records are already free
// of numbers and names (logsafe), so the ring can be shown as is.
type LogRing struct {
	mu    sync.Mutex
	lines []LogLine
	size  int
	next  uint64
}

// NewLogRing keeps up to size lines.
func NewLogRing(size int) *LogRing {
	if size <= 0 {
		size = 1000
	}
	return &LogRing{size: size, next: 1}
}

func (r *LogRing) add(at time.Time, level, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, LogLine{Seq: r.next, At: at, Level: level, Text: text})
	r.next++
	if over := len(r.lines) - r.size; over > 0 {
		r.lines = append(r.lines[:0:0], r.lines[over:]...)
	}
}

// Since returns the lines with Seq > after and the Seq to ask for next.
func (r *LogRing) Since(after uint64) ([]LogLine, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []LogLine
	for _, l := range r.lines {
		if l.Seq > after {
			out = append(out, l)
		}
	}
	return out, r.next - 1
}

// Handler wraps next so every record is also stored in the ring.
func (r *LogRing) Handler(next slog.Handler) slog.Handler {
	return &ringHandler{ring: r, next: next}
}

type ringHandler struct {
	ring   *LogRing
	next   slog.Handler
	prefix string // pre-formatted attributes from WithAttrs
	groups []string
}

func (h *ringHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *ringHandler) Handle(ctx context.Context, rec slog.Record) error {
	var b strings.Builder
	b.WriteString(rec.Message)
	b.WriteString(h.prefix)
	rec.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, h.groups, a)
		return true
	})
	h.ring.add(rec.Time, rec.Level.String(), b.String())
	return h.next.Handle(ctx, rec)
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var b strings.Builder
	b.WriteString(h.prefix)
	for _, a := range attrs {
		writeAttr(&b, h.groups, a)
	}
	return &ringHandler{ring: h.ring, next: h.next.WithAttrs(attrs), prefix: b.String(), groups: h.groups}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{ring: h.ring, next: h.next.WithGroup(name), prefix: h.prefix, groups: append(append([]string{}, h.groups...), name)}
}

func writeAttr(b *strings.Builder, groups []string, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}
	fmt.Fprintf(b, " %s=%s", key, quoteIfNeeded(a.Value.Resolve().String()))
}

func quoteIfNeeded(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"=") {
		return fmt.Sprintf("%q", s)
	}
	return s
}
