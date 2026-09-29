// Package logsafe keeps personal data out of the bridge's logs: phone numbers
// are masked to their last three digits and callers' display names are not
// written, only whether there is one. log.showNumbers turns both off for
// troubleshooting.
package logsafe

import (
	"log/slog"
	"sync/atomic"
)

var showNumbers atomic.Bool

// SetShowNumbers logs numbers and names in full (log.showNumbers).
func SetShowNumbers(show bool) { showNumbers.Store(show) }

// ShowNumbers reports whether numbers and names are shown in full.
func ShowNumbers() bool { return showNumbers.Load() }

// Number masks a phone number to its last three digits, e.g. "…563". Short
// numbers (internal ones like **9) are masked completely; "" stays "".
func Number(n string) string {
	if n == "" || showNumbers.Load() {
		return n
	}
	r := []rune(n)
	if len(r) <= 3 {
		return "…"
	}
	return "…" + string(r[len(r)-3:])
}

// Numbers masks every number of a list.
func Numbers(ns []string) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = Number(n)
	}
	return out
}

// CallerName is the log attribute for a caller's display name: whether one
// exists, or the name itself with log.showNumbers.
func CallerName(name string) slog.Attr {
	if showNumbers.Load() {
		return slog.String("callerName", name)
	}
	return slog.Bool("hasCallerName", name != "")
}
