package dashboard

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

type fakeService struct {
	devices  []admin.DeviceInfo
	renamed  map[string]string
	removed  []string
	keep     bool
	pairings []string
	revoked  []string
	state    admin.PairingState
	lan      []admin.LanPairingRequest
	approved []string
	denied   []string
}

func (f *fakeService) Status() admin.Status {
	return admin.Status{
		Version: "1.0.0", Fingerprint: "hp2:fingerprint", SIPRegistered: true, Registrar: "192.168.178.1", SIPUser: "620",
		PublicIP: "203.0.113.7", PublicIPSource: "upnp", MediaPort: 50000,
		Listen: "172.30.32.1:8080", PrivateListen: "[::]:8081", LanURL: "ws://192.168.178.20:8081/v1/ws",
		APNsConfigured: true, DevicesTotal: 2, DevicesOnline: 1, StartedAt: time.Now().Add(-2 * time.Hour),
	}
}
func (f *fakeService) Devices() ([]admin.DeviceInfo, error) { return f.devices, nil }
func (f *fakeService) RenameDevice(id, name string) (admin.DeviceInfo, error) {
	for _, d := range f.devices {
		if d.ID == id {
			f.renamed[id] = name
			return d, nil
		}
	}
	return admin.DeviceInfo{}, admin.ErrNotFound
}
func (f *fakeService) RemoveDevice(id string, keep bool) (admin.RemoveResult, error) {
	f.removed, f.keep = append(f.removed, id), keep
	return admin.RemoveResult{}, nil
}
func (f *fakeService) CreatePairing(name string) (admin.PairingInfo, error) {
	f.pairings = append(f.pairings, name)
	return admin.PairingInfo{Code: "ABCDEFGHJKMNPQRS", Grouped: "ABCD-EFGH-JKMN-PQRS", Link: "housephone://pair?c=x", Fingerprint: "hp2:fingerprint", ExpiresAt: time.Now().Add(10 * time.Minute)}, nil
}
func (f *fakeService) PairingState(code string) (admin.PairingState, error) { return f.state, nil }
func (f *fakeService) RevokePairing(code string) error {
	f.revoked = append(f.revoked, code)
	return nil
}
func (f *fakeService) LanPairings() []admin.LanPairingRequest { return f.lan }
func (f *fakeService) ApproveLanPairing(id string) (admin.DeviceInfo, error) {
	for _, r := range f.lan {
		if r.ID == id {
			f.approved = append(f.approved, id)
			return admin.DeviceInfo{ID: "new", Name: r.DeviceName}, nil
		}
	}
	return admin.DeviceInfo{}, admin.ErrNotFound
}
func (f *fakeService) DenyLanPairing(id string) error {
	f.denied = append(f.denied, id)
	return nil
}
func (f *fakeService) Calls() admin.CallsView {
	return admin.CallsView{Active: []admin.CallInfo{{ID: "c1", Direction: "incoming", Number: "…123", DeviceID: "iphone", Codec: "opus", StartedAt: time.Now()}}}
}

const proxy = "172.30.32.2"

func setup(t *testing.T) (*fakeService, http.Handler, *Dashboard) {
	t.Helper()
	svc := &fakeService{
		renamed: map[string]string{},
		devices: []admin.DeviceInfo{
			{ID: "iphone", Name: "iPhone", Platform: "ios", Online: true},
			{ID: "watch", Name: "Watch", Platform: "watchos", PairedBy: "iphone", PairedByName: "iPhone"},
		},
	}
	d := New(svc, SupervisorGate{Proxy: netip.MustParseAddr(proxy)}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.PollInterval = time.Millisecond
	return svc, d.Handler(), d
}

func do(h http.Handler, method, path, from string, form url.Values, hdr map[string]string) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	r.RemoteAddr = from + ":41234"
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestOnlyIngressProxy(t *testing.T) {
	_, h, _ := setup(t)
	for _, from := range []string{"192.168.178.30", "127.0.0.1", "172.30.33.5"} {
		if w := do(h, "GET", "/", from, nil, map[string]string{"X-Forwarded-For": proxy}); w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d", from, w.Code)
		}
	}
	if w := do(h, "GET", "/", proxy, nil, nil); w.Code != http.StatusOK {
		t.Fatalf("proxy: status %d", w.Code)
	}
}

func TestOverview(t *testing.T) {
	_, h, _ := setup(t)
	w := do(h, "GET", "/", proxy, nil, map[string]string{"X-Ingress-Path": "/api/hassio_ingress/abc_DEF-1", "Accept-Language": "de-DE,de;q=0.9"})
	body := w.Body.String()
	for _, want := range []string{
		`<base href="/api/hassio_ingress/abc_DEF-1/">`, "Angemeldet", "203.0.113.7", "upnp",
		"172.30.32.1:8080", "ws://192.168.178.20:8081/v1/ws", "hp2:fingerprint", "…123", "Eingehend", "1 von 2",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview misses %q", want)
		}
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	// English without German in Accept-Language.
	if body := do(h, "GET", "/", proxy, nil, nil).Body.String(); !strings.Contains(body, "Registered") {
		t.Error("English overview missing")
	}
}

func TestBasePathRejectsForeignValues(t *testing.T) {
	_, h, _ := setup(t)
	for _, p := range []string{"https://evil.example", "/api/hassio_ingress/x/../..", `/api/hassio_ingress/a"><script>`} {
		body := do(h, "GET", "/", proxy, nil, map[string]string{"X-Ingress-Path": p}).Body.String()
		if !strings.Contains(body, `<base href="/">`) {
			t.Errorf("%q: base not reset", p)
		}
	}
}

func csrfOf(t *testing.T, h http.Handler) string {
	t.Helper()
	m := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(do(h, "GET", "/", proxy, nil, nil).Body.String())
	if m == nil {
		t.Fatal("no csrf token in page")
	}
	return m[1]
}

func TestPostsNeedCSRF(t *testing.T) {
	svc, h, _ := setup(t)
	for _, path := range []string{"/pair", "/devices/iphone/rename", "/devices/iphone/remove", "/pair/ABCD/revoke"} {
		if w := do(h, "POST", path, proxy, url.Values{"name": {"x"}}, nil); w.Code != http.StatusForbidden {
			t.Errorf("%s without token: %d", path, w.Code)
		}
		if w := do(h, "POST", path, proxy, url.Values{"name": {"x"}, "csrf": {"00"}}, nil); w.Code != http.StatusForbidden {
			t.Errorf("%s with wrong token: %d", path, w.Code)
		}
	}
	token := csrfOf(t, h)
	if w := do(h, "POST", "/pair", proxy, url.Values{"csrf": {token}}, map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Errorf("cross-site post: %d", w.Code)
	}
	if len(svc.pairings)+len(svc.removed)+len(svc.renamed)+len(svc.revoked) != 0 {
		t.Fatal("a rejected post changed state")
	}
}

func TestPairFlow(t *testing.T) {
	svc, h, _ := setup(t)
	token := csrfOf(t, h)
	w := do(h, "POST", "/pair", proxy, url.Values{"csrf": {token}, "name": {"iPad"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "ABCD-EFGH-JKMN-PQRS") || !strings.Contains(body, `src="data:image/png;base64,`) {
		t.Fatalf("pair page: %d %s", w.Code, body)
	}
	if len(svc.pairings) != 1 || svc.pairings[0] != "iPad" {
		t.Errorf("pairings = %v", svc.pairings)
	}
	svc.state = admin.PairingState{Used: true, Device: &admin.DeviceInfo{Name: "iPad"}}
	w = do(h, "GET", "/pair/ABCDEFGHJKMNPQRS/state?wait=1", proxy, nil, nil)
	if !strings.Contains(w.Body.String(), `"used":true`) {
		t.Errorf("state = %s", w.Body.String())
	}
	svc.state = admin.PairingState{}
	start := time.Now()
	if w := do(h, "GET", "/pair/ABCDEFGHJKMNPQRS/state?wait=1", proxy, nil, nil); w.Code != http.StatusOK || time.Since(start) < 900*time.Millisecond {
		t.Errorf("pending wait: %d after %v", w.Code, time.Since(start))
	}
	if w := do(h, "POST", "/pair/ABCDEFGHJKMNPQRS/revoke", proxy, url.Values{"csrf": {token}}, nil); w.Code != http.StatusSeeOther || len(svc.revoked) != 1 {
		t.Errorf("revoke: %d %v", w.Code, svc.revoked)
	}
}

func TestRenameAndRemove(t *testing.T) {
	svc, h, _ := setup(t)
	token := csrfOf(t, h)
	hdr := map[string]string{"X-Ingress-Path": "/api/hassio_ingress/tok"}
	w := do(h, "POST", "/devices/iphone/rename", proxy, url.Values{"csrf": {token}, "name": {"Mein iPhone"}}, hdr)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/api/hassio_ingress/tok/" || svc.renamed["iphone"] != "Mein iPhone" {
		t.Errorf("rename: %d %q %v", w.Code, w.Header().Get("Location"), svc.renamed)
	}
	if w := do(h, "POST", "/devices/nope/rename", proxy, url.Values{"csrf": {token}, "name": {"x"}}, nil); w.Code != http.StatusNotFound {
		t.Errorf("rename unknown: %d", w.Code)
	}
	// The confirmation lists the watch paired through the iPhone.
	confirm := do(h, "GET", "/devices/iphone/remove", proxy, nil, nil).Body.String()
	if !strings.Contains(confirm, "Watch") || !strings.Contains(confirm, "keepCompanions") {
		t.Errorf("confirm page: %s", confirm)
	}
	if w := do(h, "GET", "/devices/nope/remove", proxy, nil, nil); w.Code != http.StatusNotFound {
		t.Errorf("confirm unknown: %d", w.Code)
	}
	if w := do(h, "POST", "/devices/iphone/remove", proxy, url.Values{"csrf": {token}, "keepCompanions": {"1"}}, nil); w.Code != http.StatusSeeOther || len(svc.removed) != 1 || !svc.keep {
		t.Errorf("remove: %d %v keep=%v", w.Code, svc.removed, svc.keep)
	}
}

func TestLanPairingRequests(t *testing.T) {
	svc, h, _ := setup(t)
	if body := do(h, "GET", "/", proxy, nil, nil).Body.String(); strings.Contains(body, "lan/") {
		t.Fatal("request section without requests")
	}
	svc.lan = []admin.LanPairingRequest{{ID: "r1", DeviceName: "iPhone Test", Platform: "ios", IP: "192.168.0.30", SAS: "123456", KeyFingerprint: "ABCDEF", ExpiresAt: time.Now().Add(time.Minute)}}
	body := do(h, "GET", "/", proxy, nil, map[string]string{"Accept-Language": "de"}).Body.String()
	for _, want := range []string{"123 456", "iPhone Test", "192.168.0.30", `action="lan/r1/approve"`, "Code stimmt – freigeben"} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview misses %q", want)
		}
	}
	token := csrfOf(t, h)
	if w := do(h, "POST", "/lan/r1/approve", proxy, url.Values{"csrf": {"00"}}, nil); w.Code != http.StatusForbidden || len(svc.approved) != 0 {
		t.Fatalf("approve without csrf: %d", w.Code)
	}
	if w := do(h, "POST", "/lan/r1/approve", proxy, url.Values{"csrf": {token}}, nil); w.Code != http.StatusSeeOther || len(svc.approved) != 1 {
		t.Fatalf("approve: %d %v", w.Code, svc.approved)
	}
	if w := do(h, "POST", "/lan/gone/approve", proxy, url.Values{"csrf": {token}}, nil); w.Code != http.StatusNotFound {
		t.Fatalf("approve unknown: %d", w.Code)
	}
	if w := do(h, "POST", "/lan/r1/deny", proxy, url.Values{"csrf": {token}}, nil); w.Code != http.StatusSeeOther || len(svc.denied) != 1 {
		t.Fatalf("deny: %d %v", w.Code, svc.denied)
	}
	if w := do(h, "POST", "/lan/r1/approve", "192.168.0.30", url.Values{"csrf": {token}}, nil); w.Code != http.StatusForbidden {
		t.Fatalf("approve from outside the ingress proxy: %d", w.Code)
	}
}

func TestStaticAssets(t *testing.T) {
	_, h, _ := setup(t)
	for _, p := range []string{"/static/app.css", "/static/app.js", "/static/icon.png"} {
		if w := do(h, "GET", p, proxy, nil, nil); w.Code != http.StatusOK {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
}
