package config

import (
	"net"
	"testing"
)

func TestTrustedNetworkNets(t *testing.T) {
	contains := func(nets []*net.IPNet, ip string) bool {
		for _, n := range nets {
			if n.Contains(net.ParseIP(ip)) {
				return true
			}
		}
		return false
	}
	home, err := Bridge{}.TrustedNetworkNets()
	if err != nil {
		t.Fatal(err)
	}
	tail, err := Bridge{Tailscale: true}.TrustedNetworkNets()
	if err != nil {
		t.Fatal(err)
	}
	custom, err := Bridge{TrustedNetworks: []string{"192.168.178.0/24"}, Tailscale: true}.TrustedNetworkNets()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ip                 string
		home, tail, custom bool
	}{
		{"192.168.178.20", true, true, true},
		{"192.168.1.20", true, true, false},
		{"10.0.0.1", true, true, false},
		{"172.31.255.1", true, true, false},
		{"fd12::1", true, true, false},
		{"fe80::1", true, true, false},
		{"169.254.0.1", true, true, false},
		// Loopback never by default: cloudflared connects from there.
		{"127.0.0.1", false, false, false},
		{"::1", false, false, false},
		{"100.101.102.103", false, true, true},
		{"fd7a:115c:a1e0::1", true, true, true},
		{"8.8.8.8", false, false, false},
	} {
		if got := contains(home, c.ip); got != c.home {
			t.Errorf("default %s = %v", c.ip, got)
		}
		if got := contains(tail, c.ip); got != c.tail {
			t.Errorf("tailscale %s = %v", c.ip, got)
		}
		if got := contains(custom, c.ip); got != c.custom {
			t.Errorf("custom %s = %v", c.ip, got)
		}
	}
	if _, err := (Bridge{TrustedNetworks: []string{"nope"}}).TrustedNetworkNets(); err == nil {
		t.Fatal("invalid entry accepted")
	}
}

func TestValidatePairNeedsPrivateListener(t *testing.T) {
	cfg := Default()
	cfg.Bridge.PublicURL = "wss://phone.example.com/v1/ws"
	if err := cfg.ValidatePair(); err != nil {
		t.Fatal(err)
	}
	cfg.Bridge.PrivateListen = ""
	if err := cfg.ValidatePair(); err == nil {
		t.Fatal("pairing without private listener accepted")
	}
	cfg.Bridge.LanURL = "http://x"
	if err := cfg.ValidatePair(); err == nil {
		t.Fatal("invalid lanUrl accepted")
	}
	cfg.Bridge.LanURL = "ws://192.168.178.20:8081/v1/ws"
	if err := cfg.ValidatePair(); err != nil {
		t.Fatal(err)
	}
}
