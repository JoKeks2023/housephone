package signaling

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Tests for the signaling v1.1 additions used by the watch.

const (
	testPublicURL = "wss://phone.example.com/v1/ws"
	watchTopic    = "com.jorisconrad.housephone.watchkitapp.voip"
)

func (ts *testServer) request(t *testing.T, method, path string, header http.Header, body any) (*http.Response, []byte) {
	t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
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

func TestHTTPPairing(t *testing.T) {
	ts := newTestServer(t)
	pc, err := ts.pairing.Create("", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	res, body := ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.1"),
		protocol.Pair{Code: strings.ToLower(pc.Code), DeviceName: "Apple Watch", Platform: protocol.PlatformWatchOS, Model: "Watch7,1"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	var ok protocol.PairOK
	if err := json.Unmarshal(body, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.BridgeID != "bridge-1" || ok.BridgeName != "Zuhause" || len(ok.DeviceSecret) != 43 {
		t.Fatalf("pair.ok %+v", ok)
	}
	dev, err := ts.devices.Get(ok.DeviceID)
	if err != nil || dev.Platform != protocol.PlatformWatchOS || dev.Name != "Apple Watch" {
		t.Fatalf("device %+v %v", dev, err)
	}

	// The code is single-use.
	res, body = ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.1"), protocol.Pair{Code: pc.Code, DeviceName: "x", Platform: protocol.PlatformWatchOS})
	if res.StatusCode != http.StatusForbidden || decodeError(t, body).Code != protocol.ErrorPairingInvalid {
		t.Fatalf("reused code: %d %s", res.StatusCode, body)
	}

	// Malformed requests are 400 and do not count towards the rate limit.
	for _, bad := range []any{"{", protocol.Pair{Code: "ABC", Platform: "android"}, ""} {
		res, body = ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.2"), bad)
		if res.StatusCode != http.StatusBadRequest || decodeError(t, body).Code != protocol.ErrorBadRequest {
			t.Fatalf("bad request %v: %d %s", bad, res.StatusCode, body)
		}
	}

	// Five wrong codes from one IP → 429, other IPs are unaffected.
	for range 5 {
		ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.3"), protocol.Pair{Code: "WRONGCODE2", DeviceName: "x", Platform: protocol.PlatformWatchOS})
	}
	res, body = ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.3"), protocol.Pair{Code: "WRONGCODE2", DeviceName: "x", Platform: protocol.PlatformWatchOS})
	if res.StatusCode != http.StatusTooManyRequests || decodeError(t, body).Code != protocol.ErrorPairingRateLimited {
		t.Fatalf("rate limit: %d %s", res.StatusCode, body)
	}
	res, _ = ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.4"), protocol.Pair{Code: "WRONGCODE2", DeviceName: "x", Platform: protocol.PlatformWatchOS})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("other IP: %d", res.StatusCode)
	}
}

func TestHTTPDeviceUpdateAndDelete(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)

	token, env := "0a0b0c", protocol.PushEnvironmentProduction
	update := protocol.DeviceUpdate{PushToken: &token, PushEnvironment: &env, MediaCapabilities: []string{protocol.MediaWebSocketPCMA, "carrier-pigeon"}}
	topic := watchTopic
	update.PushTopic = &topic

	res, body := ts.request(t, http.MethodPut, "/v1/device", nil, update)
	if res.StatusCode != http.StatusUnauthorized || decodeError(t, body).Code != protocol.ErrorUnauthorized {
		t.Fatalf("without auth: %d %s", res.StatusCode, body)
	}
	wrong := http.Header{"Authorization": {"Bearer " + ok.DeviceID + ".wrongsecretwrongsecretwrongsecretwrongsecr"}}
	if res, _ = ts.request(t, http.MethodPut, "/v1/device", wrong, update); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong secret: %d", res.StatusCode)
	}

	res, body = ts.request(t, http.MethodPut, "/v1/device", bearer(ok), update)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("update: %d %s", res.StatusCode, body)
	}
	dev, _ := ts.devices.Get(ok.DeviceID)
	if dev.PushToken != token || dev.PushEnvironment != env || dev.PushTopic != watchTopic ||
		!slices.Equal(dev.MediaCapabilities, []string{protocol.MediaWebSocketPCMA}) || !dev.UsesWebSocketAudio() {
		t.Fatalf("device after update %+v", dev)
	}

	// Topics outside the app's bundle are rejected; nothing is applied.
	for _, bad := range []string{"com.evil.app.voip", "com.jorisconrad.housephone.watchkitapp", "com.jorisconrad.housephoneX.voip"} {
		badTopic, otherToken := bad, "ffff"
		res, body = ts.request(t, http.MethodPut, "/v1/device", bearer(ok), protocol.DeviceUpdate{PushTopic: &badTopic, PushToken: &otherToken})
		if res.StatusCode != http.StatusBadRequest || decodeError(t, body).Code != protocol.ErrorBadRequest {
			t.Fatalf("topic %q: %d %s", bad, res.StatusCode, body)
		}
	}
	if dev, _ = ts.devices.Get(ok.DeviceID); dev.PushTopic != watchTopic || dev.PushToken != token {
		t.Fatalf("rejected update was applied: %+v", dev)
	}

	// A partial update leaves the other fields alone.
	name := "Uhr"
	if res, _ = ts.request(t, http.MethodPut, "/v1/device", bearer(ok), protocol.DeviceUpdate{DeviceName: &name}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("partial update: %d", res.StatusCode)
	}
	if dev, _ = ts.devices.Get(ok.DeviceID); dev.Name != "Uhr" || dev.PushTopic != watchTopic || !dev.UsesWebSocketAudio() {
		t.Fatalf("partial update changed other fields: %+v", dev)
	}

	// DELETE removes the device and closes its WebSocket.
	c, _, err := ts.dial(t, bearer(ok))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypeHello, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}})
	receive(t, c, protocol.TypeWelcome, nil)
	if res, body = ts.request(t, http.MethodDelete, "/v1/device", bearer(ok), nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	if status := closeStatus(t, c); status != websocket.StatusNormalClosure {
		t.Fatalf("socket closed with %v", status)
	}
	if _, err := ts.devices.Get(ok.DeviceID); err != store.ErrDeviceNotFound {
		t.Fatalf("device still there: %v", err)
	}
	if res, _ = ts.request(t, http.MethodDelete, "/v1/device", bearer(ok), nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second delete: %d", res.StatusCode)
	}
}

func TestCompanionPairingThroughIPhone(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	c, _, err := ts.dial(t, bearer(phone))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypeHello, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformIOS})
	receive(t, c, protocol.TypeWelcome, nil)

	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Apple Watch von Joris", Platform: "tamagotchi"})
	var bad protocol.Error
	receive(t, c, protocol.TypeError, &bad)
	if bad.Code != protocol.ErrorBadRequest {
		t.Fatalf("error %+v", bad)
	}

	before := time.Now()
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Apple Watch von Joris", Platform: protocol.PlatformWatchOS})
	var companion protocol.PairCompanion
	receive(t, c, protocol.TypePairCompanion, &companion)
	if companion.URL != testPublicURL || len(companion.Code) != store.PairingCodeLength {
		t.Fatalf("pair.companion %+v", companion)
	}
	if ttl := companion.ExpiresAt.Sub(before); ttl < 9*time.Minute || ttl > 11*time.Minute || companion.ExpiresAt.Nanosecond() != 0 {
		t.Fatalf("expiresAt %v (ttl %v)", companion.ExpiresAt, ttl)
	}

	// The watch pairs itself over HTTPS with the code.
	res, body := ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.1.1.1"),
		protocol.Pair{Code: companion.Code, DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("watch pairing: %d %s", res.StatusCode, body)
	}
	var ok protocol.PairOK
	_ = json.Unmarshal(body, &ok)
	if dev, err := ts.devices.Get(ok.DeviceID); err != nil || dev.Name != "Apple Watch von Joris" || dev.ID == phone.DeviceID {
		t.Fatalf("watch device %+v %v", dev, err)
	}
}

func TestHelloStoresCapabilitiesAndTopic(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)

	connect := func(hello protocol.Hello) *websocket.Conn {
		c, _, err := ts.dial(t, bearer(ok))
		if err != nil {
			t.Fatal(err)
		}
		send(t, c, protocol.TypeHello, hello)
		receive(t, c, protocol.TypeWelcome, nil)
		return c
	}

	c := connect(protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}, PushTopic: watchTopic})
	c.CloseNow()
	dev, _ := ts.devices.Get(ok.DeviceID)
	if !dev.UsesWebSocketAudio() || dev.PushTopic != watchTopic {
		t.Fatalf("after watch hello: %+v", dev)
	}

	// An invalid topic is reported but the connection stays up; the stored
	// topic is kept.
	c = connect(protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}, PushTopic: "com.evil.voip"})
	var e protocol.Error
	receive(t, c, protocol.TypeError, &e)
	if e.Code != protocol.ErrorBadRequest {
		t.Fatalf("error %+v", e)
	}
	send(t, c, protocol.TypeCallAttach, protocol.CallAttach{CallID: "x"})
	select {
	case <-ts.hub.messages:
	case <-time.After(2 * time.Second):
		t.Fatal("connection did not stay up after invalid topic")
	}
	c.CloseNow()
	if dev, _ = ts.devices.Get(ok.DeviceID); dev.PushTopic != watchTopic {
		t.Fatalf("invalid topic stored: %+v", dev)
	}

	// A v1 hello (iPhone app 0.1) means the defaults again.
	c = connect(protocol.Hello{AppVersion: "0.1.0 (1)", Platform: protocol.PlatformIOS})
	c.CloseNow()
	if dev, _ = ts.devices.Get(ok.DeviceID); dev.UsesWebSocketAudio() || dev.PushTopic != "" || len(dev.MediaCapabilities) != 0 {
		t.Fatalf("v1 hello should reset to defaults: %+v", dev)
	}
}

type recordingSink struct {
	mu     sync.Mutex
	frames [][]byte
	got    chan struct{}
}

func (s *recordingSink) DeviceAudio(frame []byte) {
	s.mu.Lock()
	s.frames = append(s.frames, append([]byte(nil), frame...))
	s.mu.Unlock()
	select {
	case s.got <- struct{}{}:
	default:
	}
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

func TestBinaryAudioFrames(t *testing.T) {
	ts := newTestServer(t)
	ok := ts.pairDevice(t)
	c, _, err := ts.dial(t, bearer(ok))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	send(t, c, protocol.TypeHello, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}})
	receive(t, c, protocol.TypeWelcome, nil)
	conn := ts.hub.last()

	frame := append([]byte{protocol.AudioFrameType}, bytes.Repeat([]byte{0x42}, protocol.AudioFrameBytes)...)
	writeFrame := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
			t.Fatal(err)
		}
	}

	// Without a sink (no active call) frames are ignored, the connection
	// stays. The text message after it proves the frame was processed.
	writeFrame()
	send(t, c, protocol.TypeCallAttach, protocol.CallAttach{CallID: "x"})
	select {
	case <-ts.hub.messages:
	case <-time.After(2 * time.Second):
		t.Fatal("connection did not survive a frame without sink")
	}
	sink := &recordingSink{got: make(chan struct{}, 10)}
	conn.SetAudioSink(sink)
	writeFrame()
	select {
	case <-sink.got:
	case <-time.After(2 * time.Second):
		t.Fatal("frame not routed to the sink")
	}
	if sink.count() != 1 || !bytes.Equal(sink.frames[0], frame) {
		t.Fatalf("sink got %d frames", sink.count())
	}

	// Removing another sink does nothing; removing this one stops routing.
	conn.RemoveAudioSink(&recordingSink{})
	writeFrame()
	<-sink.got
	conn.RemoveAudioSink(sink)
	writeFrame()
	time.Sleep(100 * time.Millisecond)
	if sink.count() != 2 {
		t.Fatalf("sink got %d frames after removal", sink.count())
	}

	// Bridge → device: binary message.
	out := append([]byte{protocol.AudioFrameType}, bytes.Repeat([]byte{0x24}, protocol.AudioFrameBytes)...)
	conn.SendAudio(out)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || !bytes.Equal(data, out) {
		t.Fatalf("read %v %v (%d bytes)", typ, err, len(data))
	}
}
