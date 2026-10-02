package app_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/apns"
	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/push"
	"github.com/JoKeks2023/housephone/bridge/internal/pushseal"
)

// fakeRelay records what a relay would forward to APNs.
func fakeRelay(sent chan<- apns.Notification) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req push.RelayRequest
		if r.URL.Path != push.RelayPushPath || json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, _ := json.Marshal(push.SealedPush{Sealed: req.Sealed})
		sent <- apns.Notification{Token: req.Token, Environment: req.Environment, Topic: req.Topic, CollapseID: req.CollapseID, Body: body}
	})
}

// TestEndToEndPushThroughRelay runs the bridge without its own APNs key: the
// push goes to the relay and arrives sealed for the device.
func TestEndToEndPushThroughRelay(t *testing.T) {
	sent := make(chan apns.Notification, 4)
	rs := httptest.NewServer(fakeRelay(sent))
	t.Cleanup(rs.Close)

	w := startWorldWith(t, []app.Option{app.WithPusher(nil)}, func(cfg *config.Config) {
		cfg.APNs.Relay = rs.URL
	})
	pushKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	d := w.connect(t, w.pair(t, "Test-iPhone", protocol.PlatformIOS, "iPhone17,1"))
	d.send(protocol.TypeHello, protocol.Hello{
		AppVersion: "e2e", Platform: protocol.PlatformIOS,
		PushToken: "a1b2c3d4", PushEnvironment: protocol.PushEnvironmentDevelopment,
		PushKey: base64.RawURLEncoding.EncodeToString(pushKey.PublicKey().Bytes()),
	})
	d.expect(protocol.TypeWelcome, &d.welcome)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go func() { _, _, _ = w.box.Call(ctx, "0301234567", "Oma") }()

	var incoming protocol.CallIncoming
	d.expect(protocol.TypeCallIncoming, &incoming)
	var n apns.Notification
	select {
	case n = <-sent:
	case <-time.After(wait):
		t.Fatal("no push reached the relay")
	}
	if n.Token != "a1b2c3d4" || n.Environment != apns.Development || n.Topic != "com.jorisconrad.housephone.voip" || n.CollapseID != incoming.CallID {
		t.Fatalf("notification %+v", n)
	}
	var body map[string]string
	if err := json.Unmarshal(n.Body, &body); err != nil || len(body) != 1 {
		t.Fatalf("body %s", n.Body)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(body["sealed"])
	plain, err := pushseal.Open(pushKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	var push protocol.PushIncomingCall
	if err := json.Unmarshal(plain, &push); err != nil {
		t.Fatal(err)
	}
	if push.CallID != incoming.CallID || push.Caller != "0301234567" || push.CallerName != "Oma" || push.BridgeID != w.bridge.Identity.ID {
		t.Fatalf("push %+v", push)
	}
	cancel()
}
