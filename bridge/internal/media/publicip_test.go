package media

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestStaticPublicIP(t *testing.T) {
	p := NewPublicIPSource(context.Background(), PublicIPConfig{Static: "198.51.100.4", Host: "ignored"}, discard())
	if p.Get() != "198.51.100.4" {
		t.Fatalf("got %q", p.Get())
	}
}

func TestPublicIPFromHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPublicIPSource(ctx, PublicIPConfig{Host: "localhost"}, discard())
	deadline := time.Now().Add(3 * time.Second)
	for p.Get() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.Get() != "127.0.0.1" {
		t.Fatalf("got %q", p.Get())
	}
}

// fakeSTUNServer answers binding requests with a fixed mapped address.
func fakeSTUNServer(t *testing.T, mappedIP string) string {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			req := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
			if err := req.Decode(); err != nil {
				continue
			}
			res, err := stun.Build(req, stun.BindingSuccess, &stun.XORMappedAddress{IP: net.ParseIP(mappedIP), Port: from.Port}, stun.Fingerprint)
			if err != nil {
				continue
			}
			_, _ = conn.WriteToUDP(res.Raw, from)
		}
	}()
	return "stun:" + conn.LocalAddr().String()
}

func TestSTUNPublicIP(t *testing.T) {
	server := fakeSTUNServer(t, "203.0.113.9")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ip, err := stunPublicIP(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.9" {
		t.Fatalf("got %q", ip)
	}

	resolverCtx, stop := context.WithCancel(context.Background())
	defer stop()
	p := NewPublicIPSource(resolverCtx, PublicIPConfig{Detect: true, STUN: []string{"stun:127.0.0.1:1", server}}, discard())
	deadline := time.Now().Add(8 * time.Second)
	for p.Get() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.Get() != "203.0.113.9" {
		t.Fatalf("resolver got %q", p.Get())
	}
}
