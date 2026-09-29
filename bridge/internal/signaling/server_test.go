package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

func TestHealthRevealsNothingWithoutAuth(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.request(t, http.MethodGet, "/v1/health", nil, nil)
	var plain map[string]any
	if err := json.Unmarshal(body, &plain); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("health %d %s", res.StatusCode, body)
	}
	if len(plain) != 1 || plain["status"] != "ok" {
		t.Fatalf("unauthenticated health must only say status ok, got %v", plain)
	}

	// A paired device gets the details, sealed.
	d := ts.pairDevice(t)
	authed := d.do(t, http.MethodGet, "/v1/health", nil)
	if authed.Header.Get("Content-Type") != hp2.SealedContentType {
		t.Fatalf("authenticated health not sealed: %v", authed.Header)
	}
	var full map[string]any
	if err := json.Unmarshal(authed.Body, &full); err != nil || full["sipRegistered"] != true || full["version"] != "test" {
		t.Fatalf("authenticated health %s %v", authed.Body, err)
	}
}

func TestPairingStoresOnlyThePublicKey(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	dev, err := ts.devices.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dev.Name != "iPhone Joris" || dev.Platform != "ios" || dev.Model != "iPhone17,1" {
		t.Fatalf("stored device %+v", dev)
	}
	if dev.PublicKey != hp2.B64(d.Key.PublicKeyX963()) {
		t.Fatal("stored key is not the device's public key")
	}
}

func TestPairCommandLearnsWhoUsedTheCode(t *testing.T) {
	ts := newTestServer(t)
	pc, _ := ts.pairing.Create("", time.Now())
	d := ts.pairWith(t, pc.Code, protocol.PlatformIOS, "iPhone", "iPhone17,1")
	used, found, err := ts.pairing.UsedBy(pc.Code, time.Now())
	if err != nil || !found || used.DeviceID != d.ID {
		t.Fatalf("UsedBy = %+v %v %v", used, found, err)
	}
}

// rawPair posts a pairing request built with key, optionally modified.
func (ts *testServer) rawPair(t *testing.T, ip string, req protocol.PairRequest) (*http.Response, []byte) {
	t.Helper()
	return ts.request(t, http.MethodPost, "/v1/pair", ipHeader(ip), req)
}

func newPairRequest(t *testing.T, code string) protocol.PairRequest {
	t.Helper()
	key, _ := hp2.NewSoftwareKey()
	req, err := hp2.NewPairRequest(key, code, "iPhone", protocol.PlatformIOS, "iPhone17,1")
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestPairingAttacks(t *testing.T) {
	ts := newTestServer(t)
	pc, _ := ts.pairing.Create("", time.Now())

	// A proof that does not match the key (or the nonce) is rejected with
	// 403 before the code is consumed.
	forged := newPairRequest(t, pc.Code)
	other, _ := hp2.NewSoftwareKey()
	forged.PublicKey = hp2.B64(other.PublicKeyX963())
	if res, body := ts.rawPair(t, "10.9.0.1", forged); res.StatusCode != http.StatusForbidden || decodeError(t, body).Code != protocol.ErrorPairingInvalid {
		t.Fatalf("foreign key: %d %s", res.StatusCode, body)
	}
	wrongNonce := newPairRequest(t, pc.Code)
	wrongNonce.Nonce = hp2.B64(make([]byte, hp2.NonceSize))
	if res, _ := ts.rawPair(t, "10.9.0.1", wrongNonce); res.StatusCode != http.StatusForbidden {
		t.Fatalf("proof over another nonce: %d", res.StatusCode)
	}
	// The code is still usable by the real device …
	d := ts.pairWith(t, pc.Code, protocol.PlatformIOS, "iPhone", "")
	// … exactly once.
	if res, body := ts.rawPair(t, "10.9.0.2", newPairRequest(t, pc.Code)); res.StatusCode != http.StatusForbidden || decodeError(t, body).Code != protocol.ErrorPairingInvalid {
		t.Fatalf("code used twice: %d %s", res.StatusCode, body)
	}
	if _, err := ts.devices.Get(d.ID); err != nil {
		t.Fatal(err)
	}

	// Malformed keys are 400.
	bad := newPairRequest(t, pc.Code)
	bad.PublicKey = "AAAA"
	if res, body := ts.rawPair(t, "10.9.0.3", bad); res.StatusCode != http.StatusBadRequest || decodeError(t, body).Code != protocol.ErrorBadRequest {
		t.Fatalf("malformed key: %d %s", res.StatusCode, body)
	}
}

func TestPairingRejectsInvalidCodeAndRateLimits(t *testing.T) {
	ts := newTestServer(t)
	for range 5 {
		if res, body := ts.rawPair(t, "198.51.100.1", newPairRequest(t, "WRONGCODEWRONGCO")); res.StatusCode != http.StatusForbidden {
			t.Fatalf("wrong code: %d %s", res.StatusCode, body)
		}
	}
	pc, _ := ts.pairing.Create("", time.Now())
	if res, body := ts.rawPair(t, "198.51.100.1", newPairRequest(t, pc.Code)); res.StatusCode != http.StatusTooManyRequests || decodeError(t, body).Code != protocol.ErrorPairingRateLimited {
		t.Fatalf("expected rate limit, got %d %s", res.StatusCode, body)
	}
}

func TestWebSocketWithoutValidAuthIsNotUpgraded(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	for name, header := range map[string]http.Header{
		"none":   nil,
		"bearer": {"Authorization": {"Bearer " + d.ID + ".c2VjcmV0"}},
		"junk":   {"Authorization": {"HP2 id=" + d.ID + ", ts=1, nonce=a, epk=b, sig=c"}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, res, err := websocket.Dial(ctx, ts.wsURL(), &websocket.DialOptions{HTTPHeader: header})
		cancel()
		if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401 without upgrade, got %v %v", name, res, err)
		}
	}
}

func TestRequestAttacks(t *testing.T) {
	// The bridge clock can be frozen for the clock window checks.
	var frozenAt atomic.Pointer[time.Time]
	ts := newTestServer(t, func(c *Config) {
		c.Now = func() time.Time {
			if f := frozenAt.Load(); f != nil {
				return *f
			}
			return time.Now()
		}
	})
	d := ts.pairDevice(t)

	// A captured request cannot be replayed.
	creq, err := hp2.NewClientRequest(d.Key, d.ID, testBridgeID, http.MethodGet, "/v1/health", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	auth := http.Header{"Authorization": {creq.Authorization()}}
	if res, _ := ts.request(t, http.MethodGet, "/v1/health", auth, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("first use: %d", res.StatusCode)
	}
	res, body := ts.request(t, http.MethodGet, "/v1/health", auth, nil)
	if res.StatusCode != http.StatusUnauthorized || decodeError(t, body).Code != protocol.ErrorUnauthorized {
		t.Fatalf("replay: %d %s", res.StatusCode, body)
	}
	// Even the rejection is signed by the bridge.
	if _, err := creq.VerifyAnswer(ts.identity.PublicKey(), res.StatusCode, body, res.Header.Get(hp2.BridgeHeader)); err != nil {
		t.Fatalf("401 not signed: %v", err)
	}

	// ts outside ±60 s: signature valid, but clock_skew. The bridge clock
	// is frozen on a full second so the boundary is exact.
	frozen := time.Now().Truncate(time.Second)
	frozenAt.Store(&frozen)
	for _, skew := range []time.Duration{61 * time.Second, -61 * time.Second} {
		skewed := *d
		skewed.now = func() time.Time { return frozen.Add(skew) }
		res, err := skewed.tryDo(http.MethodGet, "/v1/health", nil, nil)
		if err != nil || res.Status != http.StatusUnauthorized {
			t.Fatalf("skew %v: %d %v", skew, res.Status, err)
		}
		var e protocol.Error
		_ = json.Unmarshal(res.Body, &e)
		if e.Code != protocol.ErrorClockSkew {
			t.Fatalf("skew %v: code %q", skew, e.Code)
		}
	}
	for _, skew := range []time.Duration{60 * time.Second, -60 * time.Second} {
		within := *d
		within.now = func() time.Time { return frozen.Add(skew) }
		if res, err := within.tryDo(http.MethodGet, "/v1/health", nil, nil); err != nil || res.Status != http.StatusOK {
			t.Fatalf("skew %v rejected: %d %v", skew, res.Status, err)
		}
	}
	frozenAt.Store(nil)

	// Signed with another key for the same device ID.
	impostor := *d
	impostor.Key, _ = hp2.NewSoftwareKey()
	if res, err := impostor.tryDo(http.MethodGet, "/v1/health", nil, nil); err != nil || res.Status != http.StatusUnauthorized {
		t.Fatalf("foreign key: %d %v", res.Status, err)
	}

	// Body changed after signing.
	token := "abcd"
	signed, _ := json.Marshal(protocol.DeviceUpdate{PushToken: &token})
	creq, _ = hp2.NewClientRequest(d.Key, d.ID, testBridgeID, http.MethodPut, "/v1/device", signed, time.Now())
	other := "ffff"
	res, _ = ts.request(t, http.MethodPut, "/v1/device", http.Header{"Authorization": {creq.Authorization()}}, protocol.DeviceUpdate{PushToken: &other})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered body: %d", res.StatusCode)
	}
	// Signed for another path.
	creq, _ = hp2.NewClientRequest(d.Key, d.ID, testBridgeID, http.MethodGet, "/v1/history", nil, time.Now())
	if res, _ = ts.request(t, http.MethodGet, "/v1/health", http.Header{"Authorization": {creq.Authorization()}}, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other path: %d", res.StatusCode)
	}
	if dev, _ := ts.devices.Get(d.ID); dev.PushToken != "" {
		t.Fatalf("rejected update applied: %+v", dev)
	}
}

// Even an answer the bridge gives before checking the request (body too
// large) is signed, so the app can tell it from a forged one.
func TestOversizedBodyAnswerIsSigned(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	body := []byte(`{"deviceName":"` + strings.Repeat("x", maxHTTPBody) + `"}`)
	res, err := d.tryDo(http.MethodPut, "/v1/device", json.RawMessage(body), nil)
	if err != nil || res.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d %v", res.Status, err)
	}
}

// Unknown paths get a signed 404; 401 answers carry Date (the app derives
// the clock offset from it).
func TestNotFoundSignedAnd401Dated(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	res, err := d.tryDo(http.MethodGet, "/v1/nothing", nil, nil)
	if err != nil || res.Status != http.StatusNotFound {
		t.Fatalf("unknown path: %d %v", res.Status, err)
	}
	ctx, cancel := contextWithTimeout()
	defer cancel()
	_, wsRes, err := websocket.Dial(ctx, ts.wsURL(), nil)
	if err == nil || wsRes == nil || wsRes.StatusCode != http.StatusUnauthorized || wsRes.Header.Get("Date") == "" {
		t.Fatalf("401 upgrade: %v %v", wsRes, err)
	}
}

// dialRaw opens the WebSocket like hp2.Client.Dial but returns the keys, so
// a test can write frames the real client never would.
func dialRaw(t *testing.T, ts *testServer, d *testDevice) (*websocket.Conn, hp2.SessionKeys) {
	t.Helper()
	creq, err := hp2.NewClientRequest(d.Key, d.ID, testBridgeID, http.MethodGet, "/v1/ws", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, res, err := websocket.Dial(ctx, ts.wsURL(), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {creq.Authorization()}}})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := creq.VerifyAnswer(ts.identity.PublicKey(), res.StatusCode, nil, res.Header.Get(hp2.BridgeHeader))
	if err != nil {
		t.Fatal(err)
	}
	return ws, keys
}

func TestFrameIntegrityAttacks(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	hello, _ := protocol.MustEnvelope(protocol.TypeHello, protocol.Hello{AppVersion: "1"}).Marshal()
	helloFrame := append([]byte{hp2.FrameJSON}, hello...)
	attach, _ := protocol.MustEnvelope(protocol.TypeCallAttach, protocol.CallAttach{CallID: "x"}).Marshal()
	attachFrame := append([]byte{hp2.FrameJSON}, attach...)

	write := func(ws *websocket.Conn, typ websocket.MessageType, data []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := ws.Write(ctx, typ, data); err != nil {
			t.Fatal(err)
		}
	}
	expectClose := func(name string, ws *websocket.Conn) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for {
			if _, _, err := ws.Read(ctx); err != nil {
				if got := websocket.CloseStatus(err); got != protocol.CloseIntegrity {
					t.Fatalf("%s: closed with %v, want 4002", name, got)
				}
				return
			}
		}
	}
	started := func() (*websocket.Conn, *hp2.Sealer) {
		ws, keys := dialRaw(t, ts, d)
		sealer, _ := hp2.NewSealer(keys.DeviceToBridge)
		sealed, _ := sealer.Seal(helloFrame)
		write(ws, websocket.MessageBinary, sealed)
		return ws, sealer
	}

	// A plaintext text frame (a v1 client, or injection) instead of hello.
	ws, _ := dialRaw(t, ts, d)
	write(ws, websocket.MessageText, hello)
	expectClose("text frame", ws)

	// A sealed frame with a flipped bit.
	ws, sealer := started()
	sealed, _ := sealer.Seal(attachFrame)
	sealed[3] ^= 0x80
	write(ws, websocket.MessageBinary, sealed)
	expectClose("tampered frame", ws)

	// The same sealed frame twice (replay within the connection).
	ws, sealer = started()
	sealed, _ = sealer.Seal(attachFrame)
	write(ws, websocket.MessageBinary, sealed)
	write(ws, websocket.MessageBinary, sealed)
	expectClose("replayed frame", ws)

	// A frame skipped (counter 2 before 1: reordered or dropped).
	ws, sealer = started()
	_, _ = sealer.Seal(attachFrame)
	sealed, _ = sealer.Seal(attachFrame)
	write(ws, websocket.MessageBinary, sealed)
	expectClose("reordered frame", ws)

	// A frame sealed with the bridge's direction key (reflected frame).
	ws, keys := dialRaw(t, ts, d)
	wrongDir, _ := hp2.NewSealer(keys.BridgeToDevice)
	sealed, _ = wrongDir.Seal(helloFrame)
	write(ws, websocket.MessageBinary, sealed)
	expectClose("reflected frame", ws)
}

func TestAuthenticatedSessionFlow(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	c, err := ts.dial(t, d)
	if err != nil {
		t.Fatal(err)
	}
	defer c.WS().CloseNow()
	send(t, c, protocol.TypeHello, protocol.Hello{AppVersion: "0.1.0 (1)", Platform: protocol.PlatformIOS, PushToken: "abcd", PushEnvironment: protocol.PushEnvironmentDevelopment})
	var welcome protocol.Welcome
	receive(t, c, protocol.TypeWelcome, &welcome)
	if welcome.BridgeName != "Zuhause" || !welcome.SIPRegistered {
		t.Fatalf("welcome %+v", welcome)
	}
	dev, _ := ts.devices.Get(d.ID)
	if dev.PushToken != "abcd" || dev.PushEnvironment != "development" {
		t.Fatalf("push token not stored: %+v", dev)
	}

	// Invalid tokens are ignored, valid updates applied.
	bad, good := "NOT-HEX", "00ff"
	send(t, c, protocol.TypeDeviceUpdate, protocol.DeviceUpdate{PushToken: &bad})
	send(t, c, protocol.TypeDeviceUpdate, protocol.DeviceUpdate{PushToken: &good})
	deadline := time.Now().Add(2 * time.Second)
	for {
		dev, _ = ts.devices.Get(d.ID)
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
	if conn == nil || conn.DeviceID() != d.ID {
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
		typ, plaintext, err := c.Read(ctx)
		if err == nil && typ == hp2.FrameJSON {
			env, _ := protocol.ParseEnvelope(plaintext[1:])
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
	d := ts.pairDevice(t)
	c := ts.connectHello(t, d, protocol.Hello{})
	defer c.WS().CloseNow()
	// Not reading → no pongs → the server gives up.
	select {
	case <-ts.hub.gone:
	case <-time.After(3 * time.Second):
		t.Fatal("unresponsive client not disconnected")
	}
}

func TestSecondConnectionReplacesFirst(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	first := ts.connectHello(t, d, protocol.Hello{})
	defer first.WS().CloseNow()
	second := ts.connectHello(t, d, protocol.Hello{})
	defer second.WS().CloseNow()

	if status := closeStatus(t, first); status != protocol.CloseReplaced {
		t.Fatalf("first connection closed with %v, want 4001", status)
	}
	select {
	case gone := <-ts.hub.gone:
		if gone.DeviceID() != d.ID {
			t.Fatal("wrong device")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hub not told about replaced connection")
	}
}

func TestHelloRequiredFirst(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	c, err := ts.dial(t, d)
	if err != nil {
		t.Fatal(err)
	}
	defer c.WS().CloseNow()
	send(t, c, protocol.TypeCallAttach, protocol.CallAttach{CallID: "x"})
	var e protocol.Error
	receive(t, c, protocol.TypeError, &e)
	if e.Code != protocol.ErrorBadRequest {
		t.Fatalf("code %s", e.Code)
	}
}

func TestRemovedDeviceCannotConnect(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	if err := ts.devices.Remove(d.ID); err != nil {
		t.Fatal(err)
	}
	_, err := ts.dial(t, d)
	var se *hp2.StatusError
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 after removal, got %v", err)
	}
}

func TestDeviceUnpairRemovesDeviceAndCloses(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	c := ts.connectHello(t, d, protocol.Hello{PushToken: "abcd", PushEnvironment: protocol.PushEnvironmentProduction})
	defer c.WS().CloseNow()
	send(t, c, protocol.TypeDeviceUnpair, protocol.DeviceUnpair{})
	if status := closeStatus(t, c); status != websocket.StatusNormalClosure {
		t.Fatalf("close %v, want 1000", status)
	}
	if _, err := ts.devices.Get(d.ID); err == nil {
		t.Fatal("device (and its push token) still stored")
	}
	if _, err := ts.dial(t, d); err == nil {
		t.Fatal("unpaired device can still connect")
	}
}

func TestNewPairingIsAnnouncedToOtherDevices(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	c := ts.connectHello(t, phone, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformIOS})
	defer c.WS().CloseNow()

	pc, _ := ts.pairing.Create("Unbekanntes Gerät", time.Now())
	ts.pairWith(t, pc.Code, protocol.PlatformIOS, "iPhone", "iPhone16,2")
	var paired protocol.DevicePaired
	receive(t, c, protocol.TypeDevicePaired, &paired)
	if paired.DeviceName != "Unbekanntes Gerät" || paired.Platform != protocol.PlatformIOS || time.Since(paired.PairedAt) > time.Minute {
		t.Fatalf("device.paired %+v", paired)
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

func TestNonceCacheIsBounded(t *testing.T) {
	c := newNonceCache()
	now := time.Now()
	for i := range maxNoncesPerDevice {
		if !c.use("d", strconv.Itoa(i), now) {
			t.Fatalf("nonce %d rejected", i)
		}
	}
	if c.use("d", "one-more", now) {
		t.Fatal("full cache accepted another live nonce")
	}
	if !c.use("d", "later", now.Add(hp2.NonceWindow)) {
		t.Fatal("expired entries not evicted")
	}
	if !c.use("other-device", "x", now) {
		t.Fatal("cache is not per device")
	}
}
