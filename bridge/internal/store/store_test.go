package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDevicesCRUD(t *testing.T) {
	dir := t.TempDir()
	devices := NewDevices(dir)

	if list, err := devices.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty list: %v %v", list, err)
	}
	now := time.Now().UTC()
	a := Device{ID: "a", Name: "iPhone", Platform: "ios", SecretHash: "h", CreatedAt: now}
	b := Device{ID: "b", Name: "Watch", Platform: "watchos", SecretHash: "h", CreatedAt: now.Add(time.Second)}
	if err := devices.Add(b); err != nil {
		t.Fatal(err)
	}
	if err := devices.Add(a); err != nil {
		t.Fatal(err)
	}
	if err := devices.Add(a); err == nil {
		t.Fatal("duplicate add should fail")
	}
	list, err := devices.List()
	if err != nil || len(list) != 2 || list[0].ID != "a" {
		t.Fatalf("list order: %+v %v", list, err)
	}

	updated, err := devices.Update("a", func(d *Device) { d.PushToken = "tok"; d.PushEnvironment = "development" })
	if err != nil || updated.PushToken != "tok" {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if _, err := devices.Update("zzz", func(*Device) {}); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("update unknown: %v", err)
	}

	// Token refreshed in between: clearing the old one must not touch it.
	if err := devices.ClearPushToken("a", "old"); err != nil {
		t.Fatal(err)
	}
	if got, _ := devices.Get("a"); got.PushToken != "tok" {
		t.Fatalf("token cleared although it changed: %+v", got)
	}
	if err := devices.ClearPushToken("a", "tok"); err != nil {
		t.Fatal(err)
	}
	if got, _ := devices.Get("a"); got.PushToken != "" {
		t.Fatalf("token not cleared: %+v", got)
	}

	if err := devices.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if err := devices.Remove("a"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
	if _, err := devices.Get("a"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("get removed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("devices.json permissions %v", info.Mode().Perm())
	}
}

func TestDevicesConcurrentUpdates(t *testing.T) {
	devices := NewDevices(t.TempDir())
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := string(rune('a' + i))
			if err := devices.Add(Device{ID: id, CreatedAt: time.Now()}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	list, err := devices.List()
	if err != nil || len(list) != 20 {
		t.Fatalf("lost updates: %d %v", len(list), err)
	}
}

func TestPairingCodeLifecycle(t *testing.T) {
	pairing := NewPairing(t.TempDir())
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	pc, err := pairing.Create("iPhone Joris", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Code) != PairingCodeLength {
		t.Fatalf("code length %d", len(pc.Code))
	}
	for _, r := range pc.Code {
		if !strings.ContainsRune(PairingCodeAlphabet, r) {
			t.Fatalf("code %q contains %q", pc.Code, r)
		}
	}
	if !pc.ExpiresAt.Equal(now.Add(PairingTTL)) {
		t.Fatalf("expiry %v", pc.ExpiresAt)
	}

	// Lower-case input with whitespace is accepted.
	got, err := pairing.Consume(" "+strings.ToLower(pc.Code)+" ", now.Add(time.Minute))
	if err != nil || got.Name != "iPhone Joris" {
		t.Fatalf("consume: %+v %v", got, err)
	}
	// Single use.
	if _, err := pairing.Consume(pc.Code, now.Add(time.Minute)); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("second consume: %v", err)
	}
}

func TestPairingCodeExpires(t *testing.T) {
	pairing := NewPairing(t.TempDir())
	now := time.Now()
	pc, err := pairing.Create("", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pairing.Consume(pc.Code, now.Add(PairingTTL)); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("expired code accepted: %v", err)
	}
	if _, err := pairing.Consume("WRONGCODE2", now); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("unknown code: %v", err)
	}
}

func TestPairingCodesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		code, err := newPairingCode()
		if err != nil {
			t.Fatal(err)
		}
		if seen[code] {
			t.Fatalf("duplicate code %s", code)
		}
		seen[code] = true
	}
}

func TestIdentityIsStable(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateIdentity(dir)
	if err != nil || first.ID == "" {
		t.Fatalf("create: %+v %v", first, err)
	}
	second, err := LoadOrCreateIdentity(dir)
	if err != nil || second.ID != first.ID {
		t.Fatalf("not stable: %+v %+v %v", first, second, err)
	}
}
