package signaling

import (
	"fmt"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

// Security review N4: proxy headers are only believed from a trusted proxy.
func TestClientIPTrustsProxyHeadersOnlyFromTrustedProxies(t *testing.T) {
	_, office, _ := net.ParseCIDR("192.168.10.0/24")
	s := New(Config{Identity: testIdentity(t), TrustProxyHeaders: true, TrustedProxies: []*net.IPNet{office}})
	for _, tc := range []struct {
		remote, cf, xff, want string
	}{
		{"127.0.0.1:5000", "203.0.113.7", "", "203.0.113.7"},           // cloudflared on the host
		{"[::1]:5000", "", "198.51.100.1, 203.0.113.8", "203.0.113.8"}, // rightmost X-Forwarded-For entry
		{"192.168.10.4:5000", "203.0.113.9", "", "203.0.113.9"},        // configured proxy
		{"192.168.1.50:5000", "203.0.113.10", "", "192.168.1.50"},      // LAN client sets the header itself
		{"198.51.100.20:5000", "", "10.0.0.1", "198.51.100.20"},        // internet client
		{"127.0.0.1:5000", "not-an-ip", "", "127.0.0.1"},               // garbage header
		{"127.0.0.1:5000", "", "203.0.113.11, garbage", "127.0.0.1"},   // garbage rightmost entry
		{"127.0.0.1:5000", "2001:db8::1", "", "2001:db8::1"},           // IPv6 client
	} {
		r := httptest.NewRequest("GET", "/v1/ws", nil)
		r.RemoteAddr = tc.remote
		if tc.cf != "" {
			r.Header.Set("CF-Connecting-IP", tc.cf)
		}
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := s.clientIP(r); got != tc.want {
			t.Errorf("remote %s cf %q xff %q: got %s, want %s", tc.remote, tc.cf, tc.xff, got, tc.want)
		}
	}

	off := New(Config{Identity: testIdentity(t), TrustProxyHeaders: false})
	r := httptest.NewRequest("GET", "/v1/ws", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("CF-Connecting-IP", "203.0.113.7")
	if got := off.clientIP(r); got != "127.0.0.1" {
		t.Fatalf("trustProxyHeaders off: got %s", got)
	}
}

func TestRateLimiterGroupsIPv6By64(t *testing.T) {
	now := time.Now()
	l := newRateLimiter(3, time.Minute, func() time.Time { return now })
	for i := range 3 {
		l.fail(fmt.Sprintf("2001:db8:1:2::%x", i+1)) // rotating addresses of one /64
	}
	if !l.blocked("2001:db8:1:2::ffff") {
		t.Fatal("rotating IPv6 addresses inside a /64 escaped the limit")
	}
	if l.blocked("2001:db8:1:3::1") {
		t.Fatal("neighbouring /64 blocked")
	}
}

func TestRateLimiterStaysBounded(t *testing.T) {
	now := time.Now()
	l := newRateLimiter(5, time.Minute, func() time.Time { return now })
	for i := range limiterCap + 500 {
		l.fail(fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff))
		if i%1000 == 0 {
			now = now.Add(2 * time.Second) // let a sweep run
		}
	}
	l.mu.Lock()
	size := len(l.failures)
	l.mu.Unlock()
	if size > limiterCap {
		t.Fatalf("limiter holds %d keys, cap %d", size, limiterCap)
	}
}

func TestLogThrottle(t *testing.T) {
	now := time.Now()
	th := newLogThrottle(time.Minute, func() time.Time { return now })
	if ok, _ := th.allow("a"); !ok {
		t.Fatal("first message suppressed")
	}
	for range 10 {
		if ok, _ := th.allow("a"); ok {
			t.Fatal("repeat within a minute logged")
		}
	}
	if ok, _ := th.allow("b"); !ok {
		t.Fatal("other key suppressed")
	}
	now = now.Add(61 * time.Second)
	ok, suppressed := th.allow("a")
	if !ok || suppressed != 10 {
		t.Fatalf("after a minute: ok=%v suppressed=%d, want true 10", ok, suppressed)
	}
}
