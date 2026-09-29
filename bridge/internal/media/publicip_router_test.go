package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func upnpResponse(ip string) string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:GetExternalIPAddressResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">` +
		`<NewExternalIPAddress>` + ip + `</NewExternalIPAddress></u:GetExternalIPAddressResponse></s:Body></s:Envelope>`
}

// fakeRouter answers GetExternalIPAddress on the given control paths.
func fakeRouter(t *testing.T, answers map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, ok := answers[r.URL.Path]
		if !ok || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if !strings.HasSuffix(strings.Trim(r.Header.Get("SOAPAction"), `"`), "#GetExternalIPAddress") {
			http.Error(w, "bad SOAPAction", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, upnpResponse(ip))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRouterPublicIP(t *testing.T) {
	srv := fakeRouter(t, map[string]string{"/igdupnp/control/WANIPConn1": "94.114.0.9"})
	ip, err := routerPublicIP(context.Background(), srv.Client(), srv.URL)
	if err != nil || ip != "94.114.0.9" {
		t.Fatalf("got %q, %v", ip, err)
	}
}

func TestRouterPublicIPFallsBackToPPPService(t *testing.T) {
	srv := fakeRouter(t, map[string]string{"/igdupnp/control/WANPPPConn1": "94.114.0.10"})
	ip, err := routerPublicIP(context.Background(), srv.Client(), srv.URL)
	if err != nil || ip != "94.114.0.10" {
		t.Fatalf("got %q, %v", ip, err)
	}
}

func TestRouterPublicIPRejectsUnreachableAddresses(t *testing.T) {
	// What a router reports behind DS-Lite/CGNAT or while offline.
	for _, reported := range []string{"", "0.0.0.0", "10.1.2.3", "192.168.178.1", "172.16.0.1", "100.64.12.1", "169.254.1.1", "2a02:908::1", "not-an-ip"} {
		srv := fakeRouter(t, map[string]string{"/igdupnp/control/WANIPConn1": reported})
		if ip, err := routerPublicIP(context.Background(), srv.Client(), srv.URL); err == nil {
			t.Errorf("reported %q: accepted as %q", reported, ip)
		}
	}
}

func TestIsPublicIPv4(t *testing.T) {
	for ip, want := range map[string]bool{
		"94.114.195.1": true, "8.8.8.8": true, "100.63.255.255": true, "100.128.0.1": true,
		"100.64.0.1": false, "127.0.0.1": false, "10.0.0.1": false, "255.255.255.255": false, "224.0.0.1": false,
	} {
		if got := isPublicIPv4(ip); got != want {
			t.Errorf("isPublicIPv4(%q) = %v, want %v", ip, got, want)
		}
	}
}

type detectorHarness struct {
	d           *detector
	current     string
	clock       time.Time
	routerIP    string
	routerErr   error
	stunIP      string
	stunErr     error
	routerCalls int
	stunCalls   int
}

func newDetectorHarness(withRouter bool) *detectorHarness {
	h := &detectorHarness{clock: time.Unix(1_800_000_000, 0)}
	h.d = &detector{
		stunEvery: 10 * time.Minute,
		current:   func() string { return h.current },
		now:       func() time.Time { return h.clock },
		log:       discard(),
		stun: func(context.Context) (string, error) {
			h.stunCalls++
			return h.stunIP, h.stunErr
		},
	}
	if withRouter {
		h.d.router = func(context.Context) (string, error) {
			h.routerCalls++
			return h.routerIP, h.routerErr
		}
	}
	return h
}

func (h *detectorHarness) lookup(t *testing.T) (string, string, error) {
	t.Helper()
	ip, source, err := h.d.lookup(context.Background())
	if err == nil {
		h.current = ip
	}
	return ip, source, err
}

func TestDetectorPrefersRouterAndFollowsChanges(t *testing.T) {
	h := newDetectorHarness(true)
	h.routerIP = "94.114.0.1"
	if ip, source, err := h.lookup(t); err != nil || ip != "94.114.0.1" || source != "fritzbox" {
		t.Fatalf("got %q %q %v", ip, source, err)
	}
	h.routerIP = "94.114.0.2" // IP changed; next 30 s poll picks it up
	h.clock = h.clock.Add(30 * time.Second)
	if ip, _, _ := h.lookup(t); ip != "94.114.0.2" {
		t.Fatalf("change not picked up: %q", ip)
	}
	if h.stunCalls != 0 {
		t.Fatalf("STUN used although the router answered: %d calls", h.stunCalls)
	}
}

func TestDetectorFallsBackToSTUNAtMostEveryTenMinutes(t *testing.T) {
	h := newDetectorHarness(true)
	h.routerErr = errors.New("upnp disabled")
	h.stunIP = "94.114.0.3"

	if ip, source, err := h.lookup(t); err != nil || ip != "94.114.0.3" || source != "stun" {
		t.Fatalf("first lookup: %q %q %v", ip, source, err)
	}
	for range 5 { // five 30 s polls: no new STUN request
		h.clock = h.clock.Add(30 * time.Second)
		if ip, _, err := h.lookup(t); err != nil || ip != "94.114.0.3" {
			t.Fatalf("throttled lookup: %q %v", ip, err)
		}
	}
	if h.stunCalls != 1 {
		t.Fatalf("STUN calls within 10 min = %d, want 1", h.stunCalls)
	}
	h.clock = h.clock.Add(10 * time.Minute)
	h.stunIP = "94.114.0.4"
	if ip, _, _ := h.lookup(t); ip != "94.114.0.4" || h.stunCalls != 2 {
		t.Fatalf("after 10 min: %q, %d STUN calls", ip, h.stunCalls)
	}
	if h.routerCalls != 7 {
		t.Fatalf("router polled %d times, want every round (7)", h.routerCalls)
	}
}

func TestDetectorReportsErrorWhenNothingWorks(t *testing.T) {
	h := newDetectorHarness(true)
	h.routerErr = errors.New("no upnp")
	h.stunErr = errors.New("no stun")
	if _, _, err := h.lookup(t); err == nil {
		t.Fatal("expected error")
	}
	h.stunErr = nil
	h.stunIP = "94.114.0.5"
	// A failed STUN round must not block the next attempt for 10 minutes
	// while no IP is known yet.
	if ip, _, err := h.lookup(t); err == nil && ip == "94.114.0.5" {
		return
	}
	t.Fatal("STUN retry after failure was throttled while no IP was known")
}
