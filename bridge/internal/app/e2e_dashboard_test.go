package app_test

import (
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/dashboard"
)

// Without WithDashboard (plain Docker) there is no dashboard listener.
func TestEndToEndNoDashboardByDefault(t *testing.T) {
	w := startWorld(t)
	if addr := w.bridge.DashboardAddr(); addr != "" {
		t.Fatalf("dashboard listens on %s without WithDashboard", addr)
	}
}

// The add-on dashboard shows the running bridge and creates real pairing
// codes. The test gate admits loopback in place of the ingress proxy.
func TestEndToEndDashboard(t *testing.T) {
	gate := dashboard.SupervisorGate{Proxy: netip.MustParseAddr("127.0.0.1")}
	w := startWorldWith(t, []app.Option{app.WithDashboard("127.0.0.1:0", gate)})
	base := "http://" + w.bridge.DashboardAddr()

	get := func(path string) string {
		t.Helper()
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", path, res.StatusCode)
		}
		return string(body)
	}
	page := get("/")
	if !strings.Contains(page, w.bridge.Key.Fingerprint()) || !strings.Contains(page, "Registered") {
		t.Fatalf("overview lacks fingerprint or SIP state:\n%s", page)
	}
	m := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no csrf token")
	}
	res, err := http.PostForm(base+"/pair", url.Values{"csrf": {m[1]}, "name": {"iPad"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	codes := regexp.MustCompile(`data-code="([A-Z0-9]+)"`).FindStringSubmatch(string(body))
	if res.StatusCode != http.StatusOK || codes == nil {
		t.Fatalf("pair: %d\n%s", res.StatusCode, body)
	}
	pending, err := w.bridge.Pairing.Pending(time.Now())
	if err != nil || len(pending) != 1 || pending[0].Code != codes[1] {
		t.Fatalf("pending codes %+v, %v", pending, err)
	}
}
