package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// syncBuffer is written by pair and read by the test.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func pairConfig(t *testing.T) config.Config {
	cfg := config.Default()
	cfg.Bridge.DataDir = t.TempDir()
	cfg.Bridge.PublicURL = "wss://phone.example.com/v1/ws"
	cfg.Bridge.Name = "Zuhause"
	// The LAN address of the private listener (no FRITZ!Box lookup).
	cfg.SIP.BindHost = "192.168.178.20"
	return cfg
}

// startPair runs the pair command and returns the code it shows.
func startPair(t *testing.T, ctx context.Context, cfg config.Config) (string, *syncBuffer, chan error) {
	t.Helper()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- pair(ctx, cfg, "", "", out, 10*time.Millisecond) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(out.String(), "\n") {
			if code, ok := strings.CutPrefix(line, "Code:   "); ok {
				return code, out, done
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no code shown:\n%s", out.String())
	return "", nil, nil
}

func TestPairShowsV2LinkAndReportsTheDevice(t *testing.T) {
	cfg := pairConfig(t)
	code, out, done := startPair(t, context.Background(), cfg)

	// Grouped for reading aloud: XXXX-XXXX-XXXX-XXXX.
	if len(code) != 19 || strings.Count(code, "-") != 3 {
		t.Fatalf("code %q not grouped", code)
	}
	key, err := app.LoadIdentityKey(cfg.Bridge.DataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "housephone://pair?v=2&") || !strings.Contains(text, "fp="+key.Fingerprint()) ||
		!strings.Contains(text, "code="+hp2.NormalizeCode(code)) ||
		!strings.Contains(text, "lan=ws%3A%2F%2F192.168.178.20%3A8081%2Fv1%2Fws") {
		t.Fatalf("link missing or not v2:\n%s", text)
	}

	// The running bridge pairs a device with the code.
	devKey, _ := hp2.NewSoftwareKey()
	pairing := store.NewPairing(cfg.Bridge.DataDir)
	now := time.Now()
	if _, err := pairing.Consume(code, now); err != nil {
		t.Fatal(err)
	}
	dev := store.Device{ID: "dev-1", Name: "iPhone Joris", Platform: "ios", Model: "iPhone17,1", PublicKey: hp2.B64(devKey.PublicKeyX963()), CreatedAt: now}
	if err := store.NewDevices(cfg.Bridge.DataDir).Add(dev); err != nil {
		t.Fatal(err)
	}
	if err := pairing.RecordUse(code, dev.ID, now); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pair did not notice the pairing")
	}
	text = out.String()
	for _, want := range []string{"Gekoppelt:", "iPhone Joris", "iPhone17,1", "ios", hp2.KeyFingerprint(dev.PublicKey), "devices remove dev-1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report lacks %q:\n%s", want, text)
		}
	}
}

func TestPairCancelRevokesTheCode(t *testing.T) {
	cfg := pairConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	code, _, done := startPair(t, ctx, cfg)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errPairingAborted) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pair did not stop")
	}
	if _, err := store.NewPairing(cfg.Bridge.DataDir).Consume(code, time.Now()); !errors.Is(err, store.ErrPairingInvalid) {
		t.Fatalf("revoked code still usable: %v", err)
	}
}

func TestIdentityShowsTheFingerprint(t *testing.T) {
	cfg := pairConfig(t)
	var out strings.Builder
	if err := identity(cfg, &out); err == nil {
		t.Fatal("identity without a key must fail, not create one")
	}
	key, err := app.LoadIdentityKey(cfg.Bridge.DataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := identity(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), key.Fingerprint()) || !strings.Contains(out.String(), hp2.FingerprintHex(key.PublicKey())) {
		t.Fatalf("output:\n%s", out.String())
	}
}
