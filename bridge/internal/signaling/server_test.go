package signaling

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/auth"
	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

type fakeHub struct {
	mu        sync.Mutex
	connected []calls.DeviceConn
	messages  chan protocol.Envelope
	gone      chan calls.DeviceConn
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

func (h *fakeHub) last() calls.DeviceConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.connected) == 0 {
		return nil
	}
	return h.connected[len(h.connected)-1]
}

type testServer struct {
	srv     *Server
	http    *httptest.Server
	hub     *fakeHub
	devices *store.Devices
	pairing *store.Pairing
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	dir := t.TempDir()
	ts := &testServer{hub: newFakeHub(), devices: store.NewDevices(dir), pairing: store.NewPairing(dir)}
	ts.srv = New(Config{
		BridgeID: "bridge-1", BridgeName: "Zuhause", BridgeVersion: "test",
		PublicURL:         testPublicURL,
		PushTopic:         "com.jorisconrad.housephone.voip",
		TrustProxyHeaders: true,
		Devices:           ts.devices, Pairing: ts.pairing, Hub: ts.hub,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		PingInterval:     200 * time.Millisecond,
		FirstMessageWait: time.Second,
	})
	ts.http = httptest.NewServer(ts.srv.Handler())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ts.srv.Shutdown(ctx)
		ts.http.Close()
	})
	return ts
}

func (ts *testServer) wsURL() string {
	return "ws" + strings.TrimPrefix(ts.http.URL, "http") + "/v1/ws"
}

func (ts *testServer) dial(t *testing.T, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return websocket.Dial(ctx, ts.wsURL(), &websocket.DialOptions{HTTPHeader: header})
}

func send(t *testing.T, c *websocket.Conn, msgType string, payload any) {
	t.Helper()
	data, err := protocol.MustEnvelope(msgType, payload).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func receive(t *testing.T, c *websocket.Conn, msgType string, payload any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read %s: %v", msgType, err)
	}
	env, err := protocol.ParseEnvelope(data)
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

func closeStatus(t *testing.T, c *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

// pairDevice runs a successful pairing and returns the credentials.
func (ts *testServer) pairDevice(t *testing.T) protocol.PairOK {
	t.Helper()
	pc, err := ts.pairing.Create("iPhone Joris", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := ts.dial(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypePair, protocol.Pair{Code: pc.Code, DeviceName: "iPhone", Platform: protocol.PlatformIOS, Model: "iPhone17,1"})
	var ok protocol.PairOK
	receive(t, c, protocol.TypePairOK, &ok)
	if status := closeStatus(t, c); status != websocket.StatusNormalClosure {
		t.Fatalf("pairing connection closed with %v", status)
	}
	return ok
}

func bearer(ok protocol.PairOK) http.Header {
	return http.Header{"Authorization": {"Bearer " + ok.DeviceID + "." + ok.DeviceSecret}}
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	res, err := http.Get(ts.http.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["sipRegistered"] != true || body["version"] != "test" {
		t.Fatalf("health %v", body)
	}
}

func TestPairingStoresHashedSecret(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	if ok.BridgeID != "bridge-1" || ok.BridgeName != "Zuhause" || len(ok.DeviceSecret) != 43 {
		t.Fatalf("pair.ok %+v", ok)
	}
	dev, err := ts.devices.Get(ok.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if dev.Name != "iPhone Joris" || dev.Platform != "ios" || dev.Model != "iPhone17,1" {
		t.Fatalf("stored device %+v", dev)
	}
	if strings.Contains(dev.SecretHash, ok.DeviceSecret) || !auth.VerifySecret(ok.DeviceSecret, dev.SecretHash) {
		t.Fatal("secret must be stored hashed")
	}
}

func TestPairingRejectsInvalidCodeAndRateLimits(t *testing.T) {
	ts := newTestServer(t)
	try := func(code string) string {
		c, _, err := ts.dial(t, http.Header{"CF-Connecting-IP": {"198.51.100.1"}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		send(t, c, protocol.TypePair, protocol.Pair{Code: code, DeviceName: "x", Platform: protocol.PlatformIOS})
		var e protocol.Error
		receive(t, c, protocol.TypeError, &e)
		return e.Code
	}
	for range 5 {
		if got := try("WRONGCODE2"); got != protocol.ErrorPairingInvalid {
			t.Fatalf("got %s", got)
		}
	}
	pc, _ := ts.pairing.Create("", time.Now())
	if got := try(pc.Code); got != protocol.ErrorPairingRateLimited {
		t.Fatalf("expected rate limit, got %s", got)
	}
}

func TestUnauthenticatedMessagesAreRejected(t *testing.T) {
	ts := newTestServer(t)
	c, _, err := ts.dial(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypeHello, protocol.Hello{AppVersion: "1"})
	var e protocol.Error
	receive(t, c, protocol.TypeError, &e)
	if e.Code != protocol.ErrorUnauthorized {
		t.Fatalf("code %s", e.Code)
	}
	if status := closeStatus(t, c); status != websocket.StatusPolicyViolation {
		t.Fatalf("close %v", status)
	}
}

func TestWrongSecretGets401(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	ok.DeviceSecret = strings.Repeat("A", 43)
	_, res, err := ts.dial(t, bearer(ok))
	if err == nil {
		t.Fatal("expected dial error")
	}
	if res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", res)
	}
}

func TestAuthenticatedSessionFlow(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	c, _, err := ts.dial(t, bearer(ok))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypeHello, protocol.Hello{AppVersion: "0.1.0 (1)", Platform: protocol.PlatformIOS, PushToken: "abcd", PushEnvironment: protocol.PushEnvironmentDevelopment})
	var welcome protocol.Welcome
	receive(t, c, protocol.TypeWelcome, &welcome)
	if welcome.BridgeName != "Zuhause" || !welcome.SIPRegistered {
		t.Fatalf("welcome %+v", welcome)
	}
	dev, _ := ts.devices.Get(ok.DeviceID)
	if dev.PushToken != "abcd" || dev.PushEnvironment != "development" {
		t.Fatalf("push token not stored: %+v", dev)
	}

	// Invalid tokens are ignored, valid updates applied.
	bad, good := "NOT-HEX", "00ff"
	send(t, c, protocol.TypeDeviceUpdate, protocol.DeviceUpdate{PushToken: &bad})
	send(t, c, protocol.TypeDeviceUpdate, protocol.DeviceUpdate{PushToken: &good})
	deadline := time.Now().Add(2 * time.Second)
	for {
		dev, _ = ts.devices.Get(ok.DeviceID)
		if dev.PushToken == good || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if dev.PushToken != good {
		t.Fatalf("token %q", dev.PushToken)
	}

	// call.* goes to the hub; the hub can send back.
	send(t, c, protocol.TypeCallAttach, protocol.CallAttach{CallID: "x"})
	select {
	case env := <-ts.hub.messages:
		if env.Type != protocol.TypeCallAttach {
			t.Fatalf("hub got %s", env.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message not forwarded")
	}
	conn := ts.hub.last()
	if conn == nil || conn.DeviceID() != ok.DeviceID {
		t.Fatal("hub not informed about connection")
	}
	conn.Send(protocol.MustEnvelope(protocol.TypeStatus, protocol.Status{SIPRegistered: false}))
	receive(t, c, protocol.TypeStatus, nil)

	// Survives several ping intervals while the client keeps reading (as the
	// app does); pongs are answered by the client library during Read.
	incoming := make(chan protocol.Envelope, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, data, err := c.Read(ctx)
		if err == nil {
			env, _ := protocol.ParseEnvelope(data)
			incoming <- env
		}
	}()
	time.Sleep(700 * time.Millisecond)
	conn.Send(protocol.MustEnvelope(protocol.TypeStatus, protocol.Status{SIPRegistered: true}))
	select {
	case env := <-incoming:
		if env.Type != protocol.TypeStatus {
			t.Fatalf("got %s", env.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection did not survive ping intervals")
	}
}

func TestUnresponsiveClientIsDisconnectedByPing(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	c, _, err := ts.dial(t, bearer(ok))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypeHello, protocol.Hello{})
	receive(t, c, protocol.TypeWelcome, nil)
	// Not reading → no pongs → the server gives up.
	select {
	case <-ts.hub.gone:
	case <-time.After(3 * time.Second):
		t.Fatal("unresponsive client not disconnected")
	}
}

func TestSecondConnectionReplacesFirst(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	first, _, err := ts.dial(t, bearer(ok))
	if err != nil {
		t.Fatal(err)
	}
	defer first.CloseNow()
	send(t, first, protocol.TypeHello, protocol.Hello{})
	receive(t, first, protocol.TypeWelcome, nil)

	second, _, err := ts.dial(t, bearer(ok))
	if err != nil {
		t.Fatal(err)
	}
	defer second.CloseNow()
	send(t, second, protocol.TypeHello, protocol.Hello{})
	receive(t, second, protocol.TypeWelcome, nil)

	if status := closeStatus(t, first); status != protocol.CloseReplaced {
		t.Fatalf("first connection closed with %v, want 4001", status)
	}
	select {
	case gone := <-ts.hub.gone:
		if gone.DeviceID() != ok.DeviceID {
			t.Fatal("wrong device")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hub not told about replaced connection")
	}
}

func TestHelloRequiredFirst(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	c, _, err := ts.dial(t, bearer(ok))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypeCallAttach, protocol.CallAttach{CallID: "x"})
	var e protocol.Error
	receive(t, c, protocol.TypeError, &e)
	if e.Code != protocol.ErrorBadRequest {
		t.Fatalf("code %s", e.Code)
	}
}

func TestRemovedDeviceCannotConnect(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	if err := ts.devices.Remove(ok.DeviceID); err != nil {
		t.Fatal(err)
	}
	_, res, err := ts.dial(t, bearer(ok))
	if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after removal, got %v %v", res, err)
	}
}

func TestDeviceUnpairRemovesDeviceAndCloses(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	c, _, err := ts.dial(t, bearer(ok))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypeHello, protocol.Hello{PushToken: "abcd", PushEnvironment: protocol.PushEnvironmentProduction})
	receive(t, c, protocol.TypeWelcome, nil)
	send(t, c, protocol.TypeDeviceUnpair, protocol.DeviceUnpair{})
	if status := closeStatus(t, c); status != websocket.StatusNormalClosure {
		t.Fatalf("close %v, want 1000", status)
	}
	if _, err := ts.devices.Get(ok.DeviceID); err == nil {
		t.Fatal("device (and its push token) still stored")
	}
	if _, res, err := ts.dial(t, bearer(ok)); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unpaired device can still connect: %v %v", res, err)
	}
}

func TestRateLimiterWindow(t *testing.T) {
	now := time.Now()
	l := newRateLimiter(2, time.Minute, func() time.Time { return now })
	l.fail("a")
	l.fail("a")
	if !l.blocked("a") || l.blocked("b") {
		t.Fatal("limit not applied per IP")
	}
	now = now.Add(61 * time.Second)
	if l.blocked("a") {
		t.Fatal("window did not expire")
	}
}

func TestValidPushToken(t *testing.T) {
	for token, want := range map[string]bool{"00ff": true, "0": false, "GG": false, "ABCD": false, "": false} {
		if got := validPushToken(token); got != want {
			t.Errorf("validPushToken(%q) = %v", token, got)
		}
	}
}
