package signaling

import (
	"net"
	"sync"
	"time"
)

// limiterCap bounds the number of keys a rateLimiter or logThrottle keeps.
const limiterCap = 10000

// limiterKey groups addresses an attacker controls together: IPv6 clients
// usually own a whole /64, so they are limited per /64 instead of per
// address. Keys that are not IP addresses (device IDs) are used as they are.
func limiterKey(key string) string {
	ip := net.ParseIP(key)
	if ip == nil {
		return key
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// rateLimiter blocks a key (IP, /64 or device ID) after max failures within
// window.
type rateLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	failures  map[string][]time.Time
	lastSweep time.Time
}

func newRateLimiter(max int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{max: max, window: window, now: now, failures: map[string][]time.Time{}}
}

func (l *rateLimiter) recent(key string, now time.Time) []time.Time {
	var kept []time.Time
	for _, t := range l.failures[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
	} else {
		l.failures[key] = kept
	}
	return kept
}

func (l *rateLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(limiterKey(key), l.now())) >= l.max
}

func (l *rateLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	key = limiterKey(key)
	l.failures[key] = append(l.recent(key, now), now)
	l.bound(now)
}

// bound keeps the map small without scanning it on every request: expired
// keys are swept at most once per second, and if an attacker still fills it
// with fresh keys, it is reset (they gain nothing: pairing codes cannot be
// guessed in their lifetime, the limiter is only defense in depth).
func (l *rateLimiter) bound(now time.Time) {
	if len(l.failures) <= limiterCap {
		return
	}
	if now.Sub(l.lastSweep) >= time.Second {
		l.lastSweep = now
		for key := range l.failures {
			l.recent(key, now)
		}
	}
	if len(l.failures) > limiterCap {
		l.failures = map[string][]time.Time{}
	}
}

// logThrottle lets one warning per key through per interval and counts the
// suppressed ones, so failed logins cannot flood the log.
type logThrottle struct {
	interval time.Duration
	now      func() time.Time

	mu        sync.Mutex
	entries   map[string]*throttleEntry
	lastSweep time.Time
}

type throttleEntry struct {
	at         time.Time
	suppressed int
}

func newLogThrottle(interval time.Duration, now func() time.Time) *logThrottle {
	return &logThrottle{interval: interval, now: now, entries: map[string]*throttleEntry{}}
}

// allow reports whether to log now and how many messages were suppressed
// since the last one.
func (t *logThrottle) allow(key string) (bool, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if e := t.entries[key]; e != nil && now.Sub(e.at) < t.interval {
		e.suppressed++
		return false, 0
	}
	suppressed := 0
	if e := t.entries[key]; e != nil {
		suppressed = e.suppressed
	}
	t.entries[key] = &throttleEntry{at: now}
	if len(t.entries) > limiterCap {
		if now.Sub(t.lastSweep) >= time.Second {
			t.lastSweep = now
			for k, e := range t.entries {
				if now.Sub(e.at) >= t.interval {
					delete(t.entries, k)
				}
			}
		}
		if len(t.entries) > limiterCap {
			t.entries = map[string]*throttleEntry{}
		}
	}
	return true, suppressed
}
