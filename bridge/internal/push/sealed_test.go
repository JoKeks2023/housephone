package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/apns"
	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/pushseal"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

func pushKey(t *testing.T) (*ecdh.PrivateKey, string) {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k, base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
}

// openBody decrypts a sealed APNs body as the app does.
func openBody(t *testing.T, key *ecdh.PrivateKey, body []byte) protocol.PushIncomingCall {
	t.Helper()
	var outer map[string]any
	if err := json.Unmarshal(body, &outer); err != nil {
		t.Fatal(err)
	}
	if len(outer) != 1 {
		t.Fatalf("sealed body must only carry the ciphertext: %s", body)
	}
	raw, err := base64.RawURLEncoding.DecodeString(outer["sealed"].(string))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := pushseal.Open(key, raw)
	if err != nil {
		t.Fatal(err)
	}
	var p protocol.PushIncomingCall
	if err := json.Unmarshal(plain, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDirectSealsForDevicesWithPushKey(t *testing.T) {
	a, reqs, mu := newTestAPNs(t, http.StatusOK, "")
	key, pub := pushKey(t)
	payload := protocol.NewPushIncomingCall("c1", "+4930123456", "Oma", "b1")
	if err := a.PushIncomingCall(context.Background(), store.Device{ID: "d", PushToken: "abc123", PushKey: pub}, payload); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := openBody(t, key, (*reqs)[0].body); got != payload {
		t.Fatalf("decrypted %+v, want %+v", got, payload)
	}
}

type fakeSender struct {
	mu   sync.Mutex
	sent []apns.Notification
	err  error
}

func (f *fakeSender) Send(_ context.Context, n apns.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, n)
	return f.err
}

// fakeRelay behaves like the relay for what the bridge relies on: it
// accepts only sealed payloads for its topics, forwards {"sealed"} to
// APNs and maps dead tokens to 410.
func fakeRelay(sender *fakeSender, topics ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RelayRequest
		if r.URL.Path != RelayPushPath || json.NewDecoder(r.Body).Decode(&req) != nil || req.Sealed == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !slices.Contains(topics, req.Topic) {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(RelayResponse{Error: "topic not served by this relay"})
			return
		}
		body, _ := json.Marshal(SealedPush{Sealed: req.Sealed})
		err := sender.Send(r.Context(), apns.Notification{Token: req.Token, Environment: req.Environment, Topic: req.Topic, CollapseID: req.CollapseID, Body: body})
		var apnsErr *apns.Error
		switch {
		case errors.Is(err, apns.ErrInvalidToken) && errors.As(err, &apnsErr):
			w.WriteHeader(http.StatusGone)
			_ = json.NewEncoder(w).Encode(RelayResponse{Error: "device token invalid", Reason: apnsErr.Reason})
		case err != nil:
			w.WriteHeader(http.StatusBadGateway)
		}
	})
}

func newRelay(t *testing.T, sender *fakeSender) *Pusher {
	t.Helper()
	ts := httptest.NewServer(fakeRelay(sender, "com.jorisconrad.housephone.voip", "com.jorisconrad.housephone.watchkitapp.voip"))
	t.Cleanup(ts.Close)
	return NewRelayed(NewRelayClient(ts.URL+"/", ts.Client()), "com.jorisconrad.housephone.voip")
}

func TestRelayEndToEnd(t *testing.T) {
	sender := &fakeSender{}
	p := newRelay(t, sender)
	key, pub := pushKey(t)
	payload := protocol.NewPushIncomingCall("c1", "+4930123456", "Oma", "b1")
	dev := store.Device{ID: "w", PushToken: "abc123", PushEnvironment: protocol.PushEnvironmentDevelopment, PushTopic: "com.jorisconrad.housephone.watchkitapp.voip", PushKey: pub}
	if err := p.PushIncomingCall(context.Background(), dev, payload); err != nil {
		t.Fatal(err)
	}
	n := sender.sent[0]
	if n.Token != "abc123" || n.Environment != apns.Development || n.Topic != dev.PushTopic || n.CollapseID != "c1" {
		t.Fatalf("notification %+v", n)
	}
	if got := openBody(t, key, n.Body); got != payload {
		t.Fatalf("decrypted %+v", got)
	}
}

func TestRelayRefusesPlaintext(t *testing.T) {
	sender := &fakeSender{}
	p := newRelay(t, sender)
	err := p.PushIncomingCall(context.Background(), store.Device{ID: "d", PushToken: "abc123"}, protocol.PushIncomingCall{CallID: "c"})
	if !errors.Is(err, ErrNoPushKey) {
		t.Fatalf("got %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatal("plaintext reached APNs")
	}
}

func TestRelayReportsDeadToken(t *testing.T) {
	p := newRelay(t, &fakeSender{err: &apns.Error{Status: http.StatusGone, Reason: "Unregistered"}})
	_, pub := pushKey(t)
	err := p.PushIncomingCall(context.Background(), store.Device{ID: "d", PushToken: "abc123", PushKey: pub}, protocol.PushIncomingCall{CallID: "c"})
	if !errors.Is(err, calls.ErrInvalidPushToken) {
		t.Fatalf("got %v", err)
	}
}

func TestRelayForeignTopic(t *testing.T) {
	sender := &fakeSender{}
	p := newRelay(t, sender)
	_, pub := pushKey(t)
	err := p.PushIncomingCall(context.Background(), store.Device{ID: "d", PushToken: "abc123", PushKey: pub, PushTopic: "com.example.phone.voip"}, protocol.PushIncomingCall{CallID: "c"})
	if err == nil || errors.Is(err, calls.ErrInvalidPushToken) || len(sender.sent) != 0 {
		t.Fatalf("got %v, sent %d", err, len(sender.sent))
	}
}
