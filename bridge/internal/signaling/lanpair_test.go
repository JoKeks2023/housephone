package signaling

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

type lanDevice struct {
	key    *hp2.SoftwareKey
	client *hp2.LanClient
	ip     string
}

func newLanDevice(t *testing.T, ip string) *lanDevice {
	t.Helper()
	key, err := hp2.NewSoftwareKey()
	if err != nil {
		t.Fatal(err)
	}
	c, err := hp2.NewLanClient(key, "iPhone Test", protocol.PlatformIOS, "iPhone17,1")
	if err != nil {
		t.Fatal(err)
	}
	return &lanDevice{key: key, client: c, ip: ip}
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	return v
}

// start and reveal run steps 1–3 and return the pairing ID.
func (ts *testServer) lanRequest(t *testing.T, d *lanDevice) string {
	t.Helper()
	res, body := ts.request(t, "POST", "/v1/pair/lan", ipHeader(d.ip), d.client.Start())
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	offer := decodeJSON[protocol.LanPairOffer](t, body)
	reveal, err := d.client.Accept(offer)
	if err != nil {
		t.Fatal(err)
	}
	res, body = ts.request(t, "POST", "/v1/pair/lan/"+offer.PairingID+"/reveal", ipHeader(d.ip), reveal)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reveal: %d %s", res.StatusCode, body)
	}
	if st := decodeJSON[protocol.LanPairState](t, body); st.Status != protocol.LanPairPending {
		t.Fatalf("reveal state %+v", st)
	}
	return offer.PairingID
}

func (ts *testServer) lanState(t *testing.T, d *lanDevice, id string, wait int) protocol.LanPairState {
	t.Helper()
	res, body := ts.request(t, "GET", "/v1/pair/lan/"+id+"?wait="+itoa(wait), ipHeader(d.ip), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("state: %d %s", res.StatusCode, body)
	}
	return decodeJSON[protocol.LanPairState](t, body)
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestLanPairingApproved(t *testing.T) {
	ts := newTestServer(t, func(c *Config) { c.LanURL = testLanURL })
	d := newLanDevice(t, "192.168.178.30")
	id := ts.lanRequest(t, d)

	pending := ts.srv.LanPairings()
	if len(pending) != 1 {
		t.Fatalf("pending %+v", pending)
	}
	p := pending[0]
	if p.SAS != d.client.SAS() || len(p.SAS) != hp2.LanSASDigits {
		t.Fatalf("SAS bridge %q, device %q", p.SAS, d.client.SAS())
	}
	if p.DeviceName != "iPhone Test" || p.IP != d.ip || p.Model != "iPhone17,1" {
		t.Fatalf("request %+v", p)
	}

	// A long poll wakes up when the admin approves.
	done := make(chan protocol.LanPairState, 1)
	go func() { done <- ts.lanState(t, d, id, 10) }()
	time.Sleep(100 * time.Millisecond)
	dev, err := ts.srv.ApproveLanPairing(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var st protocol.LanPairState
	select {
	case st = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("long poll did not wake up")
	}
	if st.Status != protocol.LanPairApproved {
		t.Fatalf("state %+v", st)
	}
	a, err := d.client.Approval(st)
	if err != nil {
		t.Fatal(err)
	}
	if a.DeviceID != dev.ID || a.BridgeID != testBridgeID || a.PublicURL != testPublicURL || a.LanURL != testLanURL || a.BridgeName != "Zuhause" {
		t.Fatalf("approval %+v", a)
	}
	if !d.client.BridgePublicKey().Equal(ts.identity.PublicKey()) {
		t.Fatal("pinned key is not the bridge identity")
	}
	stored, err := ts.devices.Get(dev.ID)
	if err != nil || stored.PublicKey != hp2.B64(d.key.PublicKeyX963()) || stored.Platform != protocol.PlatformIOS {
		t.Fatalf("stored %+v, %v", stored, err)
	}
	if len(ts.srv.LanPairings()) != 0 {
		t.Fatal("approved request still listed")
	}
	if _, err := ts.srv.ApproveLanPairing(p.ID); err == nil {
		t.Fatal("approved twice")
	}

	// The new device logs in with its key like a QR-paired one.
	td := &testDevice{ID: dev.ID, Key: d.key, Platform: protocol.PlatformIOS, ts: ts}
	if res := td.do(t, "PUT", "/v1/device", protocol.DeviceUpdate{}); res.Status != http.StatusNoContent {
		t.Fatalf("login after LAN pairing: %d", res.Status)
	}
}

func TestLanPairingDenied(t *testing.T) {
	ts := newTestServer(t)
	d := newLanDevice(t, "192.168.178.31")
	id := ts.lanRequest(t, d)
	if err := ts.srv.DenyLanPairing(ts.srv.LanPairings()[0].ID); err != nil {
		t.Fatal(err)
	}
	if st := ts.lanState(t, d, id, 0); st.Status != protocol.LanPairDenied {
		t.Fatalf("state %+v", st)
	}
	if devs, _ := ts.devices.List(); len(devs) != 0 {
		t.Fatalf("devices %+v", devs)
	}
}

func TestLanPairingExpires(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	ts := newTestServer(t, func(c *Config) {
		c.Now = func() time.Time { return time.Unix(0, now.Load()) }
	})
	d := newLanDevice(t, "192.168.178.32")
	id := ts.lanRequest(t, d)
	now.Add(int64(DefaultLanPairTTL + time.Second))
	if len(ts.srv.LanPairings()) != 0 {
		t.Fatal("expired request still listed")
	}
	if st := ts.lanState(t, d, id, 0); st.Status != protocol.LanPairExpired {
		t.Fatalf("state %+v", st)
	}
	if _, err := ts.srv.ApproveLanPairing(id); err == nil {
		t.Fatal("approved an expired request")
	}
}

// A device that reveals values other than those it committed to is
// rejected, and the request is gone.
func TestLanPairingRejectsWrongReveal(t *testing.T) {
	ts := newTestServer(t)
	d := newLanDevice(t, "192.168.178.33")
	res, body := ts.request(t, "POST", "/v1/pair/lan", ipHeader(d.ip), d.client.Start())
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	offer := decodeJSON[protocol.LanPairOffer](t, body)
	reveal, err := d.client.Accept(offer)
	if err != nil {
		t.Fatal(err)
	}
	other, err := hp2.NewEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	reveal.DeviceEphemeral = hp2.B64(other.PublicKey().Bytes())
	res, body = ts.request(t, "POST", "/v1/pair/lan/"+offer.PairingID+"/reveal", ipHeader(d.ip), reveal)
	if res.StatusCode != http.StatusForbidden || decodeError(t, body).Code != protocol.ErrorPairingInvalid {
		t.Fatalf("wrong reveal: %d %s", res.StatusCode, body)
	}
	// No second chance with the right values.
	good, _ := d.client.Accept(offer)
	res, _ = ts.request(t, "POST", "/v1/pair/lan/"+offer.PairingID+"/reveal", ipHeader(d.ip), good)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("second reveal: %d", res.StatusCode)
	}
	if len(ts.srv.LanPairings()) != 0 {
		t.Fatal("rejected request listed")
	}
}

// A bad proof (signature of another key) is rejected as well.
func TestLanPairingRejectsBadProof(t *testing.T) {
	ts := newTestServer(t)
	d := newLanDevice(t, "192.168.178.34")
	_, body := ts.request(t, "POST", "/v1/pair/lan", ipHeader(d.ip), d.client.Start())
	offer := decodeJSON[protocol.LanPairOffer](t, body)
	reveal, _ := d.client.Accept(offer)
	reveal.Proof = hp2.B64(make([]byte, 64))
	res, _ := ts.request(t, "POST", "/v1/pair/lan/"+offer.PairingID+"/reveal", ipHeader(d.ip), reveal)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("bad proof: %d", res.StatusCode)
	}
}

// Only the address that started a request may reveal or poll it.
func TestLanPairingBoundToSource(t *testing.T) {
	ts := newTestServer(t)
	d := newLanDevice(t, "192.168.178.35")
	_, body := ts.request(t, "POST", "/v1/pair/lan", ipHeader(d.ip), d.client.Start())
	offer := decodeJSON[protocol.LanPairOffer](t, body)
	reveal, _ := d.client.Accept(offer)
	res, _ := ts.request(t, "POST", "/v1/pair/lan/"+offer.PairingID+"/reveal", ipHeader("192.168.178.99"), reveal)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("reveal from elsewhere: %d", res.StatusCode)
	}
}

func TestLanPairingLimits(t *testing.T) {
	ts := newTestServer(t)
	// Per source: lanStartsPerSource starts within the window.
	d := newLanDevice(t, "192.168.178.40")
	for i := range lanStartsPerSource + 1 {
		res, body := ts.request(t, "POST", "/v1/pair/lan", ipHeader(d.ip), d.client.Start())
		want := http.StatusOK
		if i == lanStartsPerSource {
			want = http.StatusTooManyRequests
		}
		if res.StatusCode != want {
			t.Fatalf("start %d: %d %s", i, res.StatusCode, body)
		}
	}
	// A retry from the same source replaced the earlier request: one open.
	ts.srv.lan.mu.Lock()
	open := len(ts.srv.lan.entries)
	ts.srv.lan.mu.Unlock()
	if open != 1 {
		t.Fatalf("open requests from one source: %d", open)
	}

	// Globally at most lanMaxOpen requests wait at the same time.
	for i := range lanMaxOpen {
		other := newLanDevice(t, "192.168.179."+itoa(i+1))
		res, body := ts.request(t, "POST", "/v1/pair/lan", ipHeader(other.ip), other.client.Start())
		want := http.StatusOK
		if i == lanMaxOpen-1 {
			want = http.StatusTooManyRequests
		}
		if res.StatusCode != want {
			t.Fatalf("source %d: %d %s", i, res.StatusCode, body)
		}
	}
}

func TestLanPairingRejectsBadStart(t *testing.T) {
	ts := newTestServer(t)
	d := newLanDevice(t, "192.168.178.50")
	for name, mutate := range map[string]func(*protocol.LanPairStart){
		"watch":      func(s *protocol.LanPairStart) { s.Platform = protocol.PlatformWatchOS },
		"no name":    func(s *protocol.LanPairStart) { s.DeviceName = "‎" },
		"bad key":    func(s *protocol.LanPairStart) { s.PublicKey = "AAAA" },
		"commitment": func(s *protocol.LanPairStart) { s.Commitment = hp2.B64(make([]byte, 16)) },
	} {
		start := d.client.Start()
		mutate(&start)
		res, body := ts.request(t, "POST", "/v1/pair/lan", ipHeader(d.ip), start)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
}

// The public listener refuses LAN pairing with home_network_required.
func TestLanPairingNotOnPublicListener(t *testing.T) {
	ts := newTestServer(t)
	pub := startListener(t, ts.srv.PublicHandler())
	d := newLanDevice(t, "")
	data, _ := json.Marshal(d.client.Start())
	res, err := http.Post(pub.URL+"/v1/pair/lan", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var e protocol.Error
	_ = json.NewDecoder(res.Body).Decode(&e)
	if res.StatusCode != http.StatusForbidden || e.Code != protocol.ErrorHomeNetworkRequired {
		t.Fatalf("public: %d %+v", res.StatusCode, e)
	}
}
