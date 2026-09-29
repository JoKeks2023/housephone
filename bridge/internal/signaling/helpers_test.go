package signaling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

const (
	testBridgeID  = "bridge-1"
	testPublicURL = "wss://phone.example.com/v1/ws"
	watchTopic    = "com.jorisconrad.housephone.watchkitapp.voip"
)

type fakeHub struct {
	mu        sync.Mutex
	connected []calls.DeviceConn
	messages  chan protocol.Envelope
	gone      chan calls.DeviceConn
	// statuses: CallStatus answers keyed by "<deviceID>/<callID>".
	statuses map[string]protocol.CallStatus
	revoked  []string
}

func newFakeHub() *fakeHub {
	return &fakeHub{messages: make(chan protocol.Envelope, 10), gone: make(chan calls.DeviceConn, 10)}
}

func (h *fakeHub) DeviceConnected(c calls.DeviceConn) {
	h.mu.Lock()
	h.connected = append(h.connected, c)
	h.mu.Unlock()
}
func (h *fakeHub) DeviceDisconnected(c calls.DeviceConn) { h.gone <- c }
func (h *fakeHub) HandleDeviceMessage(c calls.DeviceConn, env protocol.Envelope) {
	h.messages <- env
}
func (h *fakeHub) SIPRegistered() bool { return true }
func (h *fakeHub) DeviceRevoked(c calls.DeviceConn) {
	h.mu.Lock()
	h.revoked = append(h.revoked, c.DeviceID())
	h.mu.Unlock()
}
func (h *fakeHub) CallStatus(deviceID, callID string) (protocol.CallStatus, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.statuses[deviceID+"/"+callID]
	return st, ok
}

func (h *fakeHub) last() calls.DeviceConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.connected) == 0 {
		return nil
	}
	return h.connected[len(h.connected)-1]
}

type testServer struct {
	srv      *Server
	http     *httptest.Server
	hub      *fakeHub
	devices  *store.Devices
	pairing  *store.Pairing
	identity *hp2.Identity
}

func newTestServer(t *testing.T, opts ...func(*Config)) *testServer {
	t.Helper()
	dir := t.TempDir()
	id := testIdentity(t)
	ts := &testServer{hub: newFakeHub(), devices: store.NewDevices(dir), pairing: store.NewPairing(dir), identity: id}
	cfg := Config{
		BridgeID: testBridgeID, BridgeName: "Zuhause", BridgeVersion: "test",
		Identity:          id,
		PublicURL:         testPublicURL,
		PushTopic:         "com.jorisconrad.housephone.voip",
		TrustProxyHeaders: true,
		Devices:           ts.devices, Pairing: ts.pairing, Hub: ts.hub,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		PingInterval:     200 * time.Millisecond,
		FirstMessageWait: time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	ts.srv = New(cfg)
	ts.http = httptest.NewServer(ts.srv.Handler())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ts.srv.Shutdown(ctx)
		ts.http.Close()
	})
	return ts
}

func testIdentity(t *testing.T) *hp2.Identity {
	t.Helper()
	seed, err := hp2.NewIdentitySeed()
	if err != nil {
		t.Fatal(err)
	}
	id, err := hp2.NewIdentity(seed)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (ts *testServer) wsURL() string {
	return "ws" + ts.http.URL[len("http"):] + "/v1/ws"
}

// testDevice is a paired device with its software key.
type testDevice struct {
	ID       string
	Key      *hp2.SoftwareKey
	Platform string
	ts       *testServer
	// now, if set, is the device clock (clock skew tests).
	now func() time.Time
}

func (d *testDevice) client() *hp2.Client {
	return &hp2.Client{
		BaseURL: d.ts.http.URL, DeviceID: d.ID, BridgeID: testBridgeID,
		Key: d.Key, BridgePub: d.ts.identity.PublicKey(), HTTP: d.ts.http.Client(), Now: d.now,
	}
}

// pairDevice pairs an iPhone with a code from the pair command.
func (ts *testServer) pairDevice(t *testing.T) *testDevice {
	t.Helper()
	pc, err := ts.pairing.Create("iPhone Joris", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ts.pairWith(t, pc.Code, protocol.PlatformIOS, "iPhone", "iPhone17,1")
}

// mustCode creates a pairing code like the pair command.
func mustCode(t *testing.T, ts *testServer) string {
	t.Helper()
	pc, err := ts.pairing.Create("", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return pc.Code
}

func nowForTest() time.Time { return time.Now() }

// pairWith pairs a new device through POST /v1/pair and fails the test on
// any error.
func (ts *testServer) pairWith(t *testing.T, code, platform, name, model string) *testDevice {
	t.Helper()
	d, err := ts.tryPair(code, platform, name, model)
	if err != nil {
		t.Fatalf("pairing: %v", err)
	}
	return d
}

func (ts *testServer) tryPair(code, platform, name, model string) (*testDevice, error) {
	key, err := hp2.NewSoftwareKey()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := hp2.Pair(ctx, ts.http.Client(), ts.http.URL, key, code, ts.identity.Fingerprint(), name, platform, model)
	if err != nil {
		return nil, err
	}
	if res.BridgeID != testBridgeID || res.BridgeName != "Zuhause" {
		return nil, errors.New("unexpected bridge in pairing answer")
	}
	return &testDevice{ID: res.DeviceID, Key: key, Platform: platform, ts: ts}, nil
}

// dial opens the sealed WebSocket of d.
func (ts *testServer) dial(t *testing.T, d *testDevice) (*hp2.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return d.client().Dial(ctx)
}

// connectHello dials, sends hello and waits for welcome.
func (ts *testServer) connectHello(t *testing.T, d *testDevice, hello protocol.Hello) *hp2.Conn {
	t.Helper()
	c, err := ts.dial(t, d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.WS().CloseNow() })
	send(t, c, protocol.TypeHello, hello)
	receive(t, c, protocol.TypeWelcome, nil)
	return c
}

// do sends a signed request and checks the bridge's signature.
func (d *testDevice) do(t *testing.T, method, path string, body any) hp2.Response {
	t.Helper()
	res, err := d.tryDo(method, path, body, nil)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func (d *testDevice) tryDo(method, path string, body any, header http.Header) (hp2.Response, error) {
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return hp2.Response{}, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return d.client().Do(ctx, method, path, data, header)
}

// request sends an unsigned request (pairing, missing or forged auth).
func (ts *testServer) request(t *testing.T, method, path string, header http.Header, body any) (*http.Response, []byte) {
	t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	case []byte:
		reader = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, ts.http.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	return res, buf.Bytes()
}

func decodeError(t *testing.T, body []byte) protocol.Error {
	t.Helper()
	var e protocol.Error
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("error body %q: %v", body, err)
	}
	return e
}

func ipHeader(ip string) http.Header { return http.Header{"Cf-Connecting-Ip": {ip}} }

func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Second)
}

func send(t *testing.T, c *hp2.Conn, msgType string, payload any) {
	t.Helper()
	data, err := protocol.MustEnvelope(msgType, payload).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.WriteJSON(ctx, data); err != nil {
		t.Fatal(err)
	}
}

// readFrame returns the next opened frame.
func readFrame(t *testing.T, c *hp2.Conn) (byte, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	typ, plaintext, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return typ, plaintext
}

func receive(t *testing.T, c *hp2.Conn, msgType string, payload any) {
	t.Helper()
	typ, plaintext := readFrame(t, c)
	if typ != hp2.FrameJSON {
		t.Fatalf("got frame type %#x, want JSON %s", typ, msgType)
	}
	env, err := protocol.ParseEnvelope(plaintext[1:])
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != msgType {
		t.Fatalf("got %s %s, want %s", env.Type, env.Payload, msgType)
	}
	if payload != nil {
		if err := env.Decode(payload); err != nil {
			t.Fatal(err)
		}
	}
}

func closeStatus(t *testing.T, c *hp2.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		if _, _, err := c.WS().Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}
