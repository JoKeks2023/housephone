package signaling

import (
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

var iPhoneHello = protocol.Hello{AppVersion: "1", Platform: protocol.PlatformIOS}

func (ts *testServer) revoked() []string {
	ts.hub.mu.Lock()
	defer ts.hub.mu.Unlock()
	return append([]string(nil), ts.hub.revoked...)
}

func newWatchPairRequest(t *testing.T, code string) protocol.PairRequest {
	t.Helper()
	key, _ := hp2.NewSoftwareKey()
	req, err := hp2.NewPairRequest(key, code, "Watch", protocol.PlatformWatchOS, "Watch7,1")
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// The attack from the security review (M1): a removed device keeps its open
// connection and uses it to mint a companion code and pair a new device.
func TestRemovedDeviceCannotRepairThroughOpenConnection(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	c := ts.connectHello(t, phone, iPhoneHello)

	if err := ts.devices.Remove(phone.ID); err != nil {
		t.Fatal(err)
	}
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Backup", Platform: protocol.PlatformWatchOS})
	if status := closeStatus(t, c); status != websocket.StatusCode(protocol.CloseRevoked) {
		t.Fatalf("close %v, want %d (revoked)", status, protocol.CloseRevoked)
	}
	if got := ts.revoked(); len(got) != 1 || got[0] != phone.ID {
		t.Fatalf("hub.DeviceRevoked calls: %v", got)
	}
	codes, err := ts.pairing.Pending(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 0 {
		t.Fatalf("a removed device created %d pairing code(s)", len(codes))
	}
}

func TestRemovedDeviceIsCutOffWhileIdle(t *testing.T) {
	ts := newTestServer(t, func(c *Config) { c.RevalidateInterval = 50 * time.Millisecond })
	phone := ts.pairDevice(t)
	c := ts.connectHello(t, phone, iPhoneHello)
	if err := ts.devices.Remove(phone.ID); err != nil {
		t.Fatal(err)
	}
	if status := closeStatus(t, c); status != websocket.StatusCode(protocol.CloseRevoked) {
		t.Fatalf("close %v, want %d (revoked)", status, protocol.CloseRevoked)
	}
	if got := ts.revoked(); len(got) != 1 {
		t.Fatalf("hub.DeviceRevoked calls: %v", got)
	}
}

func TestCallMessagesFromRemovedDeviceAreNotForwarded(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	c := ts.connectHello(t, phone, iPhoneHello)
	if err := ts.devices.Remove(phone.ID); err != nil {
		t.Fatal(err)
	}
	send(t, c, protocol.TypeCallDial, protocol.CallDial{CallID: "3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93", Number: "0301234567"})
	if status := closeStatus(t, c); status != websocket.StatusCode(protocol.CloseRevoked) {
		t.Fatalf("close %v", status)
	}
	select {
	case env := <-ts.hub.messages:
		t.Fatalf("hub got %s from a removed device", env.Type)
	default:
	}
}

// A removed device's key is useless: signed requests and new connections
// are rejected.
func TestRemovedDeviceKeyIsUseless(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	if err := ts.devices.Remove(phone.ID); err != nil {
		t.Fatal(err)
	}
	if res, err := phone.tryDo(http.MethodGet, "/v1/health", nil, nil); err != nil || res.Status != http.StatusUnauthorized {
		t.Fatalf("request with removed key: %d %v", res.Status, err)
	}
	if _, err := ts.dial(t, phone); err == nil {
		t.Fatal("removed device connected")
	}
}

// A companion code requested before the iPhone was removed must not work.
func TestCompanionCodeDiesWithRequestingDevice(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	c := ts.connectHello(t, phone, iPhoneHello)
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
	var companion protocol.PairCompanion
	receive(t, c, protocol.TypePairCompanion, &companion)

	if err := ts.devices.Remove(phone.ID); err != nil {
		t.Fatal(err)
	}
	res, body := ts.rawPair(t, "10.9.9.9", newWatchPairRequest(t, companion.Code))
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("pairing with a removed parent's code: %d %s", res.StatusCode, body)
	}
}

func TestCompanionCodeRestrictions(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	c := ts.connectHello(t, phone, iPhoneHello)

	// Only for watches.
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Zweites iPhone", Platform: protocol.PlatformIOS})
	var e protocol.Error
	receive(t, c, protocol.TypeError, &e)
	if e.Code != protocol.ErrorBadRequest {
		t.Fatalf("ios companion: %+v", e)
	}

	// A new code replaces the previous one.
	var first, second protocol.PairCompanion
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
	receive(t, c, protocol.TypePairCompanion, &first)
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
	receive(t, c, protocol.TypePairCompanion, &second)
	if res, _ := ts.rawPair(t, "10.1.0.1", newWatchPairRequest(t, first.Code)); res.StatusCode != http.StatusForbidden {
		t.Fatalf("replaced code still valid: %d", res.StatusCode)
	}

	// The code only pairs a watch (and is used up by the attempt).
	if res, _ := ts.rawPair(t, "10.1.0.2", newPairRequest(t, second.Code)); res.StatusCode != http.StatusForbidden {
		t.Fatalf("companion code paired an iPhone: %d", res.StatusCode)
	}
	if res, _ := ts.rawPair(t, "10.1.0.2", newWatchPairRequest(t, second.Code)); res.StatusCode != http.StatusForbidden {
		t.Fatalf("companion code not used up: %d", res.StatusCode)
	}

	// A fresh code pairs the watch and records its parent.
	var third protocol.PairCompanion
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
	receive(t, c, protocol.TypePairCompanion, &third)
	watchDevice := ts.pairWith(t, third.Code, protocol.PlatformWatchOS, "Watch", "")
	watch, err := ts.devices.Get(watchDevice.ID)
	if err != nil || watch.PairedBy != phone.ID {
		t.Fatalf("watch %+v %v", watch, err)
	}
	receive(t, c, protocol.TypeDevicePaired, nil)

	// A watch cannot request companion codes itself.
	wc := ts.connectHello(t, watchDevice, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformWatchOS})
	send(t, wc, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch 2", Platform: protocol.PlatformWatchOS})
	receive(t, wc, protocol.TypeError, &e)
	if e.Code != protocol.ErrorBadRequest {
		t.Fatalf("watch requesting a companion: %+v", e)
	}

	// Rate limit: five codes per hour and device (three were created).
	for range 2 {
		send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
		receive(t, c, protocol.TypePairCompanion, nil)
	}
	send(t, c, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
	receive(t, c, protocol.TypeError, &e)
	if e.Code != protocol.ErrorPairingRateLimited {
		t.Fatalf("sixth code: %+v", e)
	}
}
