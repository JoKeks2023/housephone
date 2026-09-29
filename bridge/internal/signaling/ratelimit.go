package signaling

import (
	"sync"
	"time"
)

// rateLimiter blocks an IP after max failures within window.
type rateLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time
}

func newRateLimiter(max int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{max: max, window: window, now: now, failures: map[string][]time.Time{}}
}

func (l *rateLimiter) recent(ip string, now time.Time) []time.Time {
	var kept []time.Time
	for _, t := range l.failures[ip] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, ip)
	} else {
		l.failures[ip] = kept
	}
	return kept
}

func (l *rateLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip, l.now())) >= l.max
}

func (l *rateLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.failures[ip] = append(l.recent(ip, now), now)
	// Bound memory: forget idle IPs occasionally.
	if len(l.failures) > 10000 {
		for other := range l.failures {
			l.recent(other, now)
		}
	}
}
