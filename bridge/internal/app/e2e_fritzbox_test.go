package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/fritzbox/fritzboxtest"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

func (w *world) get(t *testing.T, d *device, path string, header http.Header) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+w.bridge.Addr()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", d.auth)
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res, body
}

func withFritzBoxTR064(box *fritzboxtest.Box) func(*config.Config) {
	return func(cfg *config.Config) {
		port, _ := strconv.Atoi(box.PlainPort())
		cfg.FritzBox.Host = box.Host()
		cfg.FritzBox.Port = port
		cfg.FritzBox.Username = fritzboxtest.Username
		cfg.FritzBox.Password = fritzboxtest.Password
	}
}

func sameAsHTTPFixture(t *testing.T, body []byte, fixture string) {
	t.Helper()
	want, err := os.ReadFile("../../../docs/protocol/fixtures/http/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	var g, w map[string]any
	if err := json.Unmarshal(body, &g); err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	_ = json.Unmarshal(want, &w)
	delete(g, "updatedAt")
	delete(w, "updatedAt")
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("%s differs from fixture:\n%s", fixture, body)
	}
}

// Real bridge, fake FRITZ!Box (SIP + TR-064): phonebook and call list over
// HTTPS, welcome.features, and the caller name from the FRITZ!Box phonebook
// in push and call.incoming when the INVITE has no display name.
func TestEndToEndPhonebookHistoryAndCallerName(t *testing.T) {
	tr064 := fritzboxtest.Start(t)
	w := startWorld(t, withFritzBoxTR064(tr064))
	d := w.pairAndConnect(t)

	res, body := w.get(t, d, "/v1/phonebook", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("phonebook %d %s", res.StatusCode, body)
	}
	sameAsHTTPFixture(t, body, "phonebook.json")
	etag := res.Header.Get("ETag")
	if res, _ := w.get(t, d, "/v1/phonebook", http.Header{"If-None-Match": {etag}}); res.StatusCode != http.StatusNotModified {
		t.Fatalf("If-None-Match: %d", res.StatusCode)
	}

	res, body = w.get(t, d, "/v1/history", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("history %d %s", res.StatusCode, body)
	}
	sameAsHTTPFixture(t, body, "history.json")
	res, body = w.get(t, d, "/v1/history?limit=1", nil)
	var h protocol.History
	if err := json.Unmarshal(body, &h); err != nil || res.StatusCode != http.StatusOK || len(h.Calls) != 1 {
		t.Fatalf("limit=1: %d %s", res.StatusCode, body)
	}

	// Both lists loaded: a device connecting now sees both features.
	second := w.pairAndConnect(t)
	if !reflect.DeepEqual(second.welcome.Features, []string{protocol.FeatureFritzBoxPhonebook, protocol.FeatureFritzBoxHistory}) {
		t.Fatalf("welcome features %v", second.welcome.Features)
	}

	// Incoming call from Oma's number without display name.
	callErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _, err := w.box.Call(ctx, "030123456", "")
		callErr <- err
	}()
	var incoming protocol.CallIncoming
	second.expect(protocol.TypeCallIncoming, &incoming)
	if incoming.Caller != "030123456" || incoming.CallerName != "Oma" {
		t.Fatalf("call.incoming %+v", incoming)
	}
	select {
	case push := <-w.pusher.pushes:
		if push.CallerName != "Oma" {
			t.Fatalf("push %+v", push)
		}
	case <-time.After(wait):
		t.Fatal("no push")
	}
	// Decline on both devices: the bridge answers 486 and the call ends.
	for _, dev := range []*device{d, second} {
		dev.send(protocol.TypeCallHangup, protocol.CallHangup{CallID: incoming.CallID, Reason: protocol.HangupReasonDeclined})
	}
	select {
	case err := <-callErr:
		if err == nil {
			t.Fatal("call should have been rejected")
		}
	case <-time.After(wait):
		t.Fatal("FRITZ!Box call did not end")
	}
}

// Wrong FRITZ!Box password: the bridge still runs, the endpoints answer
// 503 fritzbox_unavailable with an explanation, and no feature is announced.
func TestEndToEndFritzBoxLoginRejected(t *testing.T) {
	tr064 := fritzboxtest.Start(t)
	w := startWorld(t, withFritzBoxTR064(tr064), func(cfg *config.Config) { cfg.FritzBox.Password = "falsch" })
	d := w.pairAndConnect(t)
	for _, path := range []string{"/v1/phonebook", "/v1/history"} {
		res, body := w.get(t, d, path, nil)
		var e protocol.Error
		_ = json.Unmarshal(body, &e)
		if res.StatusCode != http.StatusServiceUnavailable || e.Code != protocol.ErrorFritzBoxUnavailable || e.Message == "" {
			t.Fatalf("%s: %d %s", path, res.StatusCode, body)
		}
	}
	if len(d.welcome.Features) != 0 {
		t.Fatalf("features with failing FRITZ!Box: %v", d.welcome.Features)
	}
}
