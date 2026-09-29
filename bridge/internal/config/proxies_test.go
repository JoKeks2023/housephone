package config

import (
	"net"
	"testing"
)

func TestTrustedProxyNets(t *testing.T) {
	b := Bridge{TrustedProxies: []string{"192.168.10.4", "10.0.0.0/8", "fd00::1"}}
	nets, err := b.TrustedProxyNets()
	if err != nil {
		t.Fatal(err)
	}
	contains := func(ip string) bool {
		for _, n := range nets {
			if n.Contains(net.ParseIP(ip)) {
				return true
			}
		}
		return false
	}
	for ip, want := range map[string]bool{"192.168.10.4": true, "192.168.10.5": false, "10.9.8.7": true, "fd00::1": true, "fd00::2": false} {
		if got := contains(ip); got != want {
			t.Errorf("%s trusted = %v, want %v", ip, got, want)
		}
	}
	if _, err := (Bridge{TrustedProxies: []string{"nope"}}).TrustedProxyNets(); err == nil {
		t.Fatal("invalid entry accepted")
	}
}
