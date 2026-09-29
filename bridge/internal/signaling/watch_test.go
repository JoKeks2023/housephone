package signaling

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Tests for the signaling v1.1 additions used by the watch, on v2.

func TestHTTPPairing(t *testing.T) {
	ts := newTestServer(t)
	pc, err := ts.pairing.Create("", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The code as typed from the grouped display, lower case: the proof
	// covers the canonical code.
	key, _ := hp2.NewSoftwareKey()
	req, err := hp2.NewPairRequest(key, pc.Code, "Apple Watch", protocol.PlatformWatchOS, "Watch7,1")
	if err != nil {
		t.Fatal(err)
	}
	req.Code = strings.ToLower(hp2.GroupCode(pc.Code))
	res, body := ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.1"), req)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	var ok protocol.PairResponse
	if err := json.Unmarshal(body, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.BridgeID != testBridgeID || ok.BridgeName != "Zuhause" {
		t.Fatalf("pairing answer %+v", ok)
	}
	if _, err := hp2.VerifyPairResponse(req, ok, ts.identity.Fingerprint()); err != nil {
		t.Fatalf("pairing answer not signed by the bridge: %v", err)
	}
	dev, err := ts.devices.Get(ok.DeviceID)
	if err != nil || dev.Platform != protocol.PlatformWatchOS || dev.Name != "Apple Watch" || dev.PublicKey != req.PublicKey {
		t.Fatalf("device %+v %v", dev, err)
	}

	// The code is single-use.
	res, body = ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.1"), newPairRequest(t, pc.Code))
	if res.StatusCode != http.StatusForbidden || decodeError(t, body).Code != protocol.ErrorPairingInvalid {
		t.Fatalf("reused code: %d %s", res.StatusCode, body)
	}

	// Malformed requests are 400 and do not count towards the rate limit.
	noKey := newPairRequest(t, "ABCDABCDABCDABCD")
	noKey.PublicKey = ""
	for _, bad := range []any{"{", protocol.PairRequest{Code: "ABC", Platform: "android"}, "", noKey} {
		for range 3 {
			res, body = ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.2"), bad)
			if res.StatusCode != http.StatusBadRequest || decodeError(t, body).Code != protocol.ErrorBadRequest {
				t.Fatalf("bad request %v: %d %s", bad, res.StatusCode, body)
			}
		}
	}

	// Five wrong codes from one IP → 429, other IPs are unaffected.
	for range 5 {
		ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.3"), newPairRequest(t, "WRONGCODEWRONGCO"))
	}
	res, body = ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.3"), newPairRequest(t, "WRONGCODEWRONGCO"))
	if res.StatusCode != http.StatusTooManyRequests || decodeError(t, body).Code != protocol.ErrorPairingRateLimited {
		t.Fatalf("rate limit: %d %s", res.StatusCode, body)
	}
	res, _ = ts.request(t, http.MethodPost, "/v1/pair", ipHeader("10.0.0.4"), newPairRequest(t, "WRONGCODEWRONGCO"))
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("other IP: %d", res.StatusCode)
	}
}

func TestHTTPDeviceUpdateAndDelete(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)

	token, env := "0a0b0c", protocol.PushEnvironmentProduction
	update := protocol.DeviceUpdate{PushToken: &token, PushEnvironment: &env, MediaCapabilities: []string{protocol.MediaWebSocketPCMA, "carrier-pigeon"}}
	topic := watchTopic
	update.PushTopic = &topic

	res, body := ts.request(t, http.MethodPut, "/v1/device", nil, update)
	if res.StatusCode != http.StatusUnauthorized || decodeError(t, body).Code != protocol.ErrorUnauthorized {
		t.Fatalf("without auth: %d %s", res.StatusCode, body)
	}
	impostor := *d
	impostor.Key, _ = hp2.NewSoftwareKey()
	if res, err := impostor.tryDo(http.MethodPut, "/v1/device", update, nil); err != nil || res.Status != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d %v", res.Status, err)
	}

	if res := d.do(t, http.MethodPut, "/v1/device", update); res.Status != http.StatusNoContent || len(res.Body) != 0 {
		t.Fatalf("update: %d %s", res.Status, res.Body)
	}
	dev, _ := ts.devices.Get(d.ID)
	if dev.PushToken != token || dev.PushEnvironment != env || dev.PushTopic != watchTopic ||
		!slices.Equal(dev.MediaCapabilities, []string{protocol.MediaWebSocketPCMA}) || !dev.UsesWebSocketAudio() {
		t.Fatalf("device after update %+v", dev)
	}

	// Topics outside the app's bundle are rejected; nothing is applied. The
	// error answer is sealed like any other.
	for _, bad := range []string{"com.evil.app.voip", "com.jorisconrad.housephone.watchkitapp", "com.jorisconrad.housephoneX.voip"} {
		badTopic, otherToken := bad, "ffff"
		res := d.do(t, http.MethodPut, "/v1/device", protocol.DeviceUpdate{PushTopic: &badTopic, PushToken: &otherToken})
		if res.Status != http.StatusBadRequest || decodeError(t, res.Body).Code != protocol.ErrorBadRequest || res.Header.Get("Content-Type") != hp2.SealedContentType {
			t.Fatalf("topic %q: %d %s", bad, res.Status, res.Body)
		}
	}
	if dev, _ = ts.devices.Get(d.ID); dev.PushTopic != watchTopic || dev.PushToken != token {
		t.Fatalf("rejected update was applied: %+v", dev)
	}

	// A partial update leaves the other fields alone.
	name := "Uhr"
	if res := d.do(t, http.MethodPut, "/v1/device", protocol.DeviceUpdate{DeviceName: &name}); res.Status != http.StatusNoContent {
		t.Fatalf("partial update: %d", res.Status)
	}
	if dev, _ = ts.devices.Get(d.ID); dev.Name != "Uhr" || dev.PushTopic != watchTopic || !dev.UsesWebSocketAudio() {
		t.Fatalf("partial update changed other fields: %+v", dev)
	}

	// DELETE removes the device and closes its WebSocket.
	c := ts.connectHello(t, d, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}})
	defer c.WS().CloseNow()
	if res := d.do(t, http.MethodDelete, "/v1/device", nil); res.Status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.Status, res.Body)
	}
	if status := closeStatus(t, c); status != websocket.StatusNormalClosure {
		t.Fatalf("socket closed with %v", status)
	}
	if _, err := ts.devices.Get(d.ID); err != store.ErrDeviceNotFound {
		t.Fatalf("device still there: %v", err)
	}
	if res, err := d.tryDo(http.MethodDelete, "/v1/device", nil, nil); err != nil || res.Status != http.StatusUnauthorized {
		t.Fatalf("second delete: %d %v", res.Status, err)
	}
}

func TestCompanionPairingThroughIPhone(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	c := ts.connectHello(t, phone, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformIOS})
	defer c.WS().CloseNow()

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

	// The iPhone's code is for a watch only.
	if _, err := ts.tryPair(companion.Code, protocol.PlatformIOS, "iPhone", ""); err == nil {
		t.Fatal("companion code paired an iPhone")
	}
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Apple Watch von Joris", Platform: protocol.PlatformWatchOS})
	receive(t, c, protocol.TypePairCompanion, &companion)

	// The watch pairs itself over HTTPS with its own key and the code; the
	// iPhone hears about it.
	watch := ts.pairWith(t, companion.Code, protocol.PlatformWatchOS, "Watch", "Watch7,1")
	dev, err := ts.devices.Get(watch.ID)
	if err != nil || dev.Name != "Apple Watch von Joris" || dev.ID == phone.ID || dev.PairedBy != phone.ID ||
		dev.PublicKey != hp2.B64(watch.Key.PublicKeyX963()) {
		t.Fatalf("watch device %+v %v", dev, err)
	}
	var paired protocol.DevicePaired
	receive(t, c, protocol.TypeDevicePaired, &paired)
	if paired.DeviceName != "Apple Watch von Joris" || paired.Platform != protocol.PlatformWatchOS {
		t.Fatalf("device.paired %+v", paired)
	}
	if res := watch.do(t, http.MethodGet, "/v1/health", nil); res.Status != http.StatusOK {
		t.Fatalf("watch cannot use its key: %d", res.Status)
	}
}

func TestHelloStoresCapabilitiesAndTopic(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)

	c := ts.connectHello(t, d, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}, PushTopic: watchTopic})
	c.WS().CloseNow()
	dev, _ := ts.devices.Get(d.ID)
	if !dev.UsesWebSocketAudio() || dev.PushTopic != watchTopic {
		t.Fatalf("after watch hello: %+v", dev)
	}

	// An invalid topic is reported but the connection stays up; the stored
	// topic is kept.
	c = ts.connectHello(t, d, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}, PushTopic: "com.evil.voip"})
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
	c.WS().CloseNow()
	if dev, _ = ts.devices.Get(d.ID); dev.PushTopic != watchTopic {
		t.Fatalf("invalid topic stored: %+v", dev)
	}

	// A hello without capabilities (iPhone) means the defaults again.
	c = ts.connectHello(t, d, protocol.Hello{AppVersion: "0.1.0 (1)", Platform: protocol.PlatformIOS})
	c.WS().CloseNow()
	if dev, _ = ts.devices.Get(d.ID); dev.UsesWebSocketAudio() || dev.PushTopic != "" || len(dev.MediaCapabilities) != 0 {
		t.Fatalf("hello without capabilities should reset to defaults: %+v", dev)
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
	d := ts.pairDevice(t)
	c := ts.connectHello(t, d, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS, MediaCapabilities: []string{protocol.MediaWebSocketPCMA}})
	defer c.WS().CloseNow()
	conn := ts.hub.last()

	frame := append([]byte{protocol.AudioFrameType}, bytes.Repeat([]byte{0x42}, protocol.AudioFrameBytes)...)
	writeFrame := func() {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		if err := c.WriteAudio(ctx, frame); err != nil {
			t.Fatal(err)
		}
	}

	// Without a sink (no active call) frames are ignored, the connection
	// stays. The JSON message after it proves the frame was processed.
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

	// Bridge → device: sealed audio frame, v1.1 format inside.
	out := append([]byte{protocol.AudioFrameType}, bytes.Repeat([]byte{0x24}, protocol.AudioFrameBytes)...)
	conn.SendAudio(out)
	typ, data := readFrame(t, c)
	if typ != hp2.FrameAudio || !bytes.Equal(data, out) {
		t.Fatalf("read type %#x (%d bytes)", typ, len(data))
	}
}

func TestHTTPCallStatus(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	callID := "3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93"
	want := protocol.CallStatus{CallID: callID, State: protocol.CallStatusEnded, Reason: protocol.EndReasonAnsweredElsewhere}
	ts.hub.mu.Lock()
	ts.hub.statuses = map[string]protocol.CallStatus{d.ID + "/" + callID: want}
	ts.hub.mu.Unlock()

	res, body := ts.request(t, http.MethodGet, "/v1/calls/"+callID, nil, nil)
	if res.StatusCode != http.StatusUnauthorized || decodeError(t, body).Code != protocol.ErrorUnauthorized {
		t.Fatalf("without auth: %d %s", res.StatusCode, body)
	}

	status := d.do(t, http.MethodGet, "/v1/calls/"+callID, nil)
	if status.Status != http.StatusOK || status.Header.Get("Cache-Control") != "no-store" || status.Header.Get("Content-Type") != hp2.SealedContentType {
		t.Fatalf("status: %d %v %s", status.Status, status.Header, status.Body)
	}
	var got protocol.CallStatus
	if err := json.Unmarshal(status.Body, &got); err != nil || got != want {
		t.Fatalf("status body %s (%v), want %+v", status.Body, err, want)
	}
	if !strings.Contains(string(status.Body), `"state":"ended"`) || strings.Contains(string(status.Body), "sipCode") {
		t.Fatalf("status JSON %s", status.Body)
	}

	unknown := d.do(t, http.MethodGet, "/v1/calls/9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d", nil)
	if unknown.Status != http.StatusNotFound || decodeError(t, unknown.Body).Code != protocol.ErrorCallNotFound {
		t.Fatalf("unknown call: %d %s", unknown.Status, unknown.Body)
	}
}
