package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/apns"
	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

type recorded struct {
	path    string
	headers http.Header
	body    []byte
}

func newTestAPNs(t *testing.T, status int, reason string) (*Pusher, *[]recorded, *sync.Mutex) {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs []recorded
	)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, recorded{path: r.URL.Path, headers: r.Header.Clone(), body: body})
		mu.Unlock()
		w.Header().Set("apns-id", "test-id")
		w.WriteHeader(status)
		if reason != "" {
			_, _ = io.WriteString(w, `{"reason":"`+reason+`"}`)
		}
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := apns.New(key, "KEY123", "T9CA6D7T8N")
	client.SetEndpoints(srv.URL, srv.Client())
	return NewDirect(client, "com.jorisconrad.housephone.voip"), &reqs, &mu
}

func TestPushSendsVoIPNotification(t *testing.T) {
	a, reqs, mu := newTestAPNs(t, http.StatusOK, "")
	dev := store.Device{ID: "d", PushToken: "abc123", PushEnvironment: protocol.PushEnvironmentDevelopment}
	payload := protocol.NewPushIncomingCall("3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93", "+4930123456", "Oma", "e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e")
	if err := a.PushIncomingCall(context.Background(), dev, payload); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*reqs) != 1 {
		t.Fatalf("requests: %d", len(*reqs))
	}
	r := (*reqs)[0]
	if r.path != "/3/device/abc123" {
		t.Fatalf("path %s", r.path)
	}
	for header, want := range map[string]string{
		"Apns-Push-Type":   "voip",
		"Apns-Collapse-Id": "3f0c2b4e-8a1d-4c6e-9b7a-2d5e8f1a0c93",
		"Apns-Priority":    "10",
		"Apns-Expiration":  "0",
		"Apns-Topic":       "com.jorisconrad.housephone.voip",
	} {
		if got := r.headers.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if !strings.HasPrefix(r.headers.Get("Authorization"), "bearer ") {
		t.Errorf("missing JWT: %q", r.headers.Get("Authorization"))
	}

	// Payload must equal the shared fixture.
	fixture, err := os.ReadFile("../../../docs/protocol/fixtures/push.incoming_call.json")
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	_ = json.Unmarshal(fixture, &want)
	if err := json.Unmarshal(r.body, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("payload\n got %s\nwant %s", r.body, fixture)
	}
}

func TestPushInvalidTokenIsReported(t *testing.T) {
	for _, tc := range []struct {
		status int
		reason string
	}{
		{http.StatusGone, "Unregistered"},
		{http.StatusBadRequest, "BadDeviceToken"},
		{http.StatusBadRequest, "DeviceTokenNotForTopic"},
	} {
		a, _, _ := newTestAPNs(t, tc.status, tc.reason)
		err := a.PushIncomingCall(context.Background(), store.Device{ID: "d", PushToken: "x"}, protocol.PushIncomingCall{})
		if !errors.Is(err, calls.ErrInvalidPushToken) {
			t.Errorf("%d %s: got %v", tc.status, tc.reason, err)
		}
	}
	a, _, _ := newTestAPNs(t, http.StatusTooManyRequests, "TooManyRequests")
	err := a.PushIncomingCall(context.Background(), store.Device{ID: "d", PushToken: "x"}, protocol.PushIncomingCall{})
	if err == nil || errors.Is(err, calls.ErrInvalidPushToken) {
		t.Fatalf("429 should be a plain error, got %v", err)
	}
}

func TestPushWithoutTokenFails(t *testing.T) {
	a, reqs, _ := newTestAPNs(t, http.StatusOK, "")
	if err := a.PushIncomingCall(context.Background(), store.Device{ID: "d"}, protocol.PushIncomingCall{}); err == nil {
		t.Fatal("expected error")
	}
	if len(*reqs) != 0 {
		t.Fatal("request sent without token")
	}
}

func TestPushUsesDeviceTopic(t *testing.T) {
	a, reqs, mu := newTestAPNs(t, http.StatusOK, "")
	dev := store.Device{ID: "watch", PushToken: "abc123", PushTopic: "com.jorisconrad.housephone.watchkitapp.voip"}
	if err := a.PushIncomingCall(context.Background(), dev, protocol.PushIncomingCall{CallID: "c"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := (*reqs)[0].headers.Get("Apns-Topic"); got != dev.PushTopic {
		t.Fatalf("topic %q, want %q", got, dev.PushTopic)
	}
}
