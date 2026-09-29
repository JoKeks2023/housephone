package app_test

import (
	"context"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
)

func TestLanURL(t *testing.T) {
	for _, c := range []struct {
		listen, lanURL, ip, want string
	}{
		{":8081", "", "192.168.178.20", "ws://192.168.178.20:8081/v1/ws"},
		{"0.0.0.0:9000", "", "10.0.0.5", "ws://10.0.0.5:9000/v1/ws"},
		{"[::]:8081", "", "fd00::5", "ws://[fd00::5]:8081/v1/ws"},
		// A listener bound to one address uses it.
		{"192.168.1.9:8081", "", "10.0.0.5", "ws://192.168.1.9:8081/v1/ws"},
		// Configured URL wins (e.g. a Tailscale name).
		{":8081", "ws://bridge.tail1234.ts.net:8081/v1/ws", "10.0.0.5", "ws://bridge.tail1234.ts.net:8081/v1/ws"},
		{"", "", "10.0.0.5", ""},
		{":8081", "", "", ""},
	} {
		cfg := config.Default()
		cfg.Bridge.PrivateListen, cfg.Bridge.LanURL = c.listen, c.lanURL
		if got := app.LanURL(cfg, c.ip); got != c.want {
			t.Errorf("LanURL(%q, %q, %q) = %q, want %q", c.listen, c.lanURL, c.ip, got, c.want)
		}
	}
}

func TestPairingLanURL(t *testing.T) {
	cfg := config.Default()
	cfg.SIP.BindHost = "192.168.178.20"
	got, err := app.PairingLanURL(context.Background(), cfg)
	if err != nil || got != "ws://192.168.178.20:8081/v1/ws" {
		t.Fatalf("PairingLanURL = %q, %v", got, err)
	}
	cfg.Bridge.PrivateListen = ""
	if _, err := app.PairingLanURL(context.Background(), cfg); err == nil {
		t.Fatal("PairingLanURL without private listener succeeded")
	}
}
