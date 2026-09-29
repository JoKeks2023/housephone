package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

func httpBase(t *testing.T, wsURL string) string {
	t.Helper()
	base, err := hp2.HTTPBase(wsURL)
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// Pairing happens on the private listener, calls on the public one: the
// public listener has no pairing route and refuses companion codes.
func TestEndToEndPublicAndPrivateListener(t *testing.T) {
	w := startWorld(t)
	if w.lanURL == "" || w.lanURL == w.url || !strings.HasPrefix(w.lanURL, "ws://127.0.0.1:") {
		t.Fatalf("lanURL %q (public %q)", w.lanURL, w.url)
	}

	// Public: no pairing (404), health yes.
	res, err := http.Post(httpBase(t, w.url)+"/v1/pair", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("public POST /v1/pair: %d, want 404", res.StatusCode)
	}
	for _, u := range []string{w.url, w.lanURL} {
		res, err := http.Get(httpBase(t, u) + "/v1/health")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("health on %s: %d", u, res.StatusCode)
		}
	}

	// Paired over lan, connected over the public URL (pairAndConnect).
	d := w.pairAndConnect(t)
	if d.welcome.LanURL != w.lanURL {
		t.Fatalf("welcome.lanUrl %q, want %q", d.welcome.LanURL, w.lanURL)
	}
	d.send(protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Apple Watch", Platform: protocol.PlatformWatchOS})
	var perr protocol.Error
	d.expect(protocol.TypeError, &perr)
	if perr.Code != protocol.ErrorHomeNetworkRequired {
		t.Fatalf("companion over public: %+v", perr)
	}

	// The same device at home: companion codes work and point to lan.
	home := *d.client
	home.BaseURL = httpBase(t, w.lanURL)
	hd := w.connect(t, &home)
	hd.send(protocol.TypeHello, protocol.Hello{AppVersion: "e2e", Platform: protocol.PlatformIOS})
	var welcome protocol.Welcome
	hd.expect(protocol.TypeWelcome, &welcome)
	hd.send(protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Apple Watch", Platform: protocol.PlatformWatchOS})
	var pc protocol.PairCompanion
	hd.expect(protocol.TypePairCompanion, &pc)
	if pc.Code == "" || pc.LanURL != w.lanURL || pc.URL != "" && pc.URL == pc.LanURL {
		t.Fatalf("pair.companion %+v", pc)
	}

	// The watch redeems the code over lan and then works over public.
	key, err := hp2.NewSoftwareKey()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	paired, err := hp2.Pair(ctx, http.DefaultClient, httpBase(t, pc.LanURL), key, pc.Code, w.bridge.Key.Fingerprint(), "Watch", protocol.PlatformWatchOS, "Watch7,1")
	if err != nil {
		t.Fatalf("watch pairing over lan: %v", err)
	}
	watch := &hp2.Client{BaseURL: httpBase(t, w.url), DeviceID: paired.DeviceID, BridgeID: paired.BridgeID, Key: key, BridgePub: paired.BridgePub}
	got, err := watch.Do(ctx, http.MethodGet, "/v1/health", nil, nil)
	if err != nil || got.Status != http.StatusOK {
		t.Fatalf("watch health over public: %v %+v", err, got)
	}
}

// With the default trusted networks the private listener refuses loopback:
// cloudflared connects from there, so a tunnel must never reach pairing.
func TestEndToEndPrivateListenerRefusesLoopback(t *testing.T) {
	w := startWorld(t, func(c *config.Config) {
		c.Bridge.TrustedNetworks = nil
		c.Bridge.Tailscale = true
	})
	for _, path := range []string{"/v1/health", "/v1/pair"} {
		method := http.MethodGet
		if path == "/v1/pair" {
			method = http.MethodPost
		}
		req, _ := http.NewRequest(method, httpBase(t, w.lanURL)+path, strings.NewReader(`{}`))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var e protocol.Error
		_ = json.Unmarshal(body, &e)
		if res.StatusCode != http.StatusForbidden || e.Code != protocol.ErrorHomeNetworkRequired {
			t.Fatalf("%s %s from loopback: %d %s", method, path, res.StatusCode, body)
		}
	}
}
