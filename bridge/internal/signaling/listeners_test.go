package signaling

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

const testLanURL = "ws://192.168.178.20:8081/v1/ws"

func mustNets(t *testing.T, b config.Bridge) []*net.IPNet {
	t.Helper()
	nets, err := b.TrustedNetworkNets()
	if err != nil {
		t.Fatal(err)
	}
	return nets
}

func TestTrustedSource(t *testing.T) {
	home := mustNets(t, config.Bridge{})
	tailnet := mustNets(t, config.Bridge{Tailscale: true})
	for _, c := range []struct {
		addr          string
		home, tailnet bool
	}{
		{"192.168.178.20:51000", true, true},
		{"10.1.2.3:1", true, true},
		{"172.20.0.1:1", true, true},
		{"169.254.10.1:1", true, true},
		{"[fd00::1]:1", true, true},
		{"[fe80::1%en0]:1", true, true},
		{"[::ffff:192.168.1.5]:1", true, true},
		// Loopback never counts: cloudflared connects from there.
		{"127.0.0.1:1", false, false},
		{"[::1]:1", false, false},
		// Tailscale CGNAT range only with bridge.tailscale.
		{"100.100.1.2:1", false, true},
		{"100.64.0.0:1", false, true},
		{"100.127.255.255:1", false, true},
		{"100.63.255.255:1", false, false},
		{"100.128.0.0:1", false, false},
		{"8.8.8.8:1", false, false},
		{"[2001:db8::1]:1", false, false},
		{"garbage", false, false},
		{"", false, false},
	} {
		if got := TrustedSource(home, c.addr); got != c.home {
			t.Errorf("home %s: %v, want %v", c.addr, got, c.home)
		}
		if got := TrustedSource(tailnet, c.addr); got != c.tailnet {
			t.Errorf("tailscale %s: %v, want %v", c.addr, got, c.tailnet)
		}
	}
}

// startListener serves h like the bridge's second listener does.
func startListener(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func publicClient(d *testDevice, base string) *hp2.Client {
	c := d.client()
	c.BaseURL = base
	c.HTTP = http.DefaultClient
	return c
}

// The public listener serves telephony for paired devices only: pairing
// answers 404, companion codes are refused, everything else works.
func TestPublicHandlerHasNoPairing(t *testing.T) {
	ts := newTestServer(t, func(c *Config) { c.LanURL = testLanURL })
	public := startListener(t, ts.srv.PublicHandler())

	res, err := http.Post(public.URL+"/v1/pair", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("public POST /v1/pair: %d", res.StatusCode)
	}
	// A valid code still does nothing over public.
	key, err := hp2.NewSoftwareKey()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := hp2.Pair(ctx, http.DefaultClient, public.URL, key, mustCode(t, ts), ts.identity.Fingerprint(), "x", protocol.PlatformIOS, ""); err == nil {
		t.Fatal("pairing over the public listener succeeded")
	}

	// Paired (privately), the device uses the public listener.
	d := ts.pairDevice(t)
	client := publicClient(d, public.URL)
	got, err := client.Do(ctx, http.MethodGet, "/v1/health", nil, nil)
	if err != nil || got.Status != http.StatusOK {
		t.Fatalf("authenticated health over public: %v %+v", err, got)
	}
	// The call status route exists (call_not_found, not a generic 404).
	got, err = client.Do(ctx, http.MethodGet, "/v1/calls/2b0f5e0c-3c1a-4f53-9a57-0f4e7d6c1a11", nil, nil)
	if err != nil || decodeError(t, got.Body).Code != protocol.ErrorCallNotFound {
		t.Fatalf("call status over public: %v %+v", err, got)
	}

	conn, err := client.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.WS().CloseNow()
	send(t, conn, protocol.TypeHello, protocol.Hello{AppVersion: "t", Platform: protocol.PlatformIOS})
	var welcome protocol.Welcome
	receive(t, conn, protocol.TypeWelcome, &welcome)
	if welcome.LanURL != testLanURL {
		t.Fatalf("welcome.lanUrl %q", welcome.LanURL)
	}
	send(t, conn, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
	var perr protocol.Error
	receive(t, conn, protocol.TypeError, &perr)
	if perr.Code != protocol.ErrorHomeNetworkRequired {
		t.Fatalf("companion over public: %+v", perr)
	}
}

// The private listener serves everything, but only to trusted sources.
func TestPrivateHandlerFiltersSources(t *testing.T) {
	ts := newTestServer(t, func(c *Config) { c.LanURL = testLanURL })

	// Defaults: the test client connects from loopback and is refused.
	refused := startListener(t, ts.srv.PrivateHandler(mustNets(t, config.Bridge{Tailscale: true})))
	for _, path := range []string{"/v1/health", "/v1/pair", "/v1/ws"} {
		method := http.MethodGet
		if path == "/v1/pair" {
			method = http.MethodPost
		}
		req, _ := http.NewRequest(method, refused.URL+path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body [512]byte
		n, _ := res.Body.Read(body[:])
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden || decodeError(t, body[:n]).Code != protocol.ErrorHomeNetworkRequired {
			t.Fatalf("%s %s from loopback: %d %s", method, path, res.StatusCode, body[:n])
		}
	}

	// Trusted: pairing and companion codes work.
	allowed := startListener(t, ts.srv.PrivateHandler(mustNets(t, config.Bridge{TrustedNetworks: []string{"127.0.0.0/8", "::1"}})))
	key, err := hp2.NewSoftwareKey()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := hp2.Pair(ctx, http.DefaultClient, allowed.URL, key, mustCode(t, ts), ts.identity.Fingerprint(), "iPhone", protocol.PlatformIOS, "")
	if err != nil {
		t.Fatalf("pairing over the private listener: %v", err)
	}
	client := &hp2.Client{BaseURL: allowed.URL, DeviceID: res.DeviceID, BridgeID: res.BridgeID, Key: key, BridgePub: res.BridgePub}
	conn, err := client.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.WS().CloseNow()
	send(t, conn, protocol.TypeHello, protocol.Hello{AppVersion: "t", Platform: protocol.PlatformIOS})
	receive(t, conn, protocol.TypeWelcome, nil)
	send(t, conn, protocol.TypePairCompanionRequest, protocol.PairCompanionRequest{DeviceName: "Watch", Platform: protocol.PlatformWatchOS})
	var pc protocol.PairCompanion
	receive(t, conn, protocol.TypePairCompanion, &pc)
	if pc.Code == "" || pc.URL != testPublicURL || pc.LanURL != testLanURL {
		t.Fatalf("pair.companion %+v", pc)
	}
}
