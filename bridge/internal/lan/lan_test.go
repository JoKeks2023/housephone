package lan

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestIsLocal(t *testing.T) {
	for ip, want := range map[string]bool{
		"192.168.178.1": true, "192.168.0.1": true, "10.0.0.1": true, "172.16.5.4": true,
		"127.0.0.1": true, "::1": true, "fd00::1": true, "fe80::1": true, "169.254.1.1": true,
		"212.42.244.122": false, "8.8.8.8": false, "100.64.0.1": false, "2a02:908::1": false, "0.0.0.0": false,
	} {
		if got := IsLocal(net.ParseIP(ip)); got != want {
			t.Errorf("IsLocal(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestResolveIPLiterals(t *testing.T) {
	ctx := context.Background()
	if ip, err := Resolve(ctx, "192.168.0.1"); err != nil || ip.String() != "192.168.0.1" {
		t.Fatalf("private literal: %v %v", ip, err)
	}
	// What fritz.box resolves to via public DNS.
	_, err := Resolve(ctx, "212.42.244.122")
	if err == nil || !strings.Contains(err.Error(), "Heimnetz") {
		t.Fatalf("public literal accepted: %v", err)
	}
}

func TestResolveLocalhostName(t *testing.T) {
	ip, err := Resolve(context.Background(), "localhost")
	if err != nil || !ip.IsLoopback() {
		t.Fatalf("localhost: %v %v", ip, err)
	}
}
