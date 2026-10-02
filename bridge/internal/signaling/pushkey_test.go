package signaling

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

func TestPushKeyIsStoredAndValidated(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())

	c := ts.connectHello(t, d, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformIOS, PushKey: key})
	c.WS().CloseNow()
	if dev, _ := ts.devices.Get(d.ID); dev.PushKey != key {
		t.Fatalf("push key not stored: %+v", dev)
	}

	// Absent keeps it; invalid keys (low order, wrong length, not base64url)
	// are ignored.
	for _, bad := range []string{"", base64.RawURLEncoding.EncodeToString(make([]byte, 32)), "AAAA", "not base64!"} {
		c = ts.connectHello(t, d, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformIOS, PushKey: bad})
		c.WS().CloseNow()
		if dev, _ := ts.devices.Get(d.ID); dev.PushKey != key {
			t.Fatalf("after %q: %+v", bad, dev)
		}
	}

	// device.update over HTTPS (the watch) replaces it.
	k2, _ := ecdh.X25519().GenerateKey(rand.Reader)
	key2 := base64.RawURLEncoding.EncodeToString(k2.PublicKey().Bytes())
	if res := d.do(t, http.MethodPut, "/v1/device", protocol.DeviceUpdate{PushKey: &key2}); res.Status != http.StatusOK && res.Status != http.StatusNoContent {
		t.Fatalf("PUT /v1/device: %d", res.Status)
	}
	if dev, _ := ts.devices.Get(d.ID); dev.PushKey != key2 {
		t.Fatalf("update not stored: %+v", dev)
	}
}
