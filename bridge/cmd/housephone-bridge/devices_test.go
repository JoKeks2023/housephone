package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

func seedDevices(t *testing.T) (*store.Devices, *store.Pairing) {
	t.Helper()
	dir := t.TempDir()
	reg, pairing := store.NewDevices(dir), store.NewPairing(dir)
	now := time.Now()
	for _, dev := range []store.Device{
		{ID: "phone", Name: "iPhone", Platform: "ios", CreatedAt: now},
		{ID: "watch", Name: "Watch", Platform: "watchos", PairedBy: "phone", CreatedAt: now.Add(time.Second)},
		{ID: "other", Name: "iPad", Platform: "ios", CreatedAt: now.Add(2 * time.Second)},
	} {
		if err := reg.Add(dev); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pairing.CreateCompanion("phone", "Watch 2", "watchos", now); err != nil {
		t.Fatal(err)
	}
	return reg, pairing
}

func TestRemoveDeviceRemovesCompanionsAndTheirCodes(t *testing.T) {
	reg, pairing := seedDevices(t)
	var out bytes.Buffer
	if err := removeDevice(reg, pairing, []string{"phone"}, &out); err != nil {
		t.Fatal(err)
	}
	list, _ := reg.List()
	if len(list) != 1 || list[0].ID != "other" {
		t.Fatalf("remaining devices %+v", list)
	}
	if codes, _ := pairing.Pending(time.Now()); len(codes) != 0 {
		t.Fatalf("companion code of the removed device survived: %+v", codes)
	}
	if !strings.Contains(out.String(), "Ebenfalls entfernt: watch") {
		t.Fatalf("output does not mention the watch:\n%s", out.String())
	}
}

func TestRemoveDeviceKeepCompanions(t *testing.T) {
	reg, pairing := seedDevices(t)
	var out bytes.Buffer
	if err := removeDevice(reg, pairing, []string{"-keep-companions", "phone"}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Get("watch"); err != nil {
		t.Fatalf("watch removed despite -keep-companions: %v", err)
	}
	if _, err := reg.Get("phone"); err == nil {
		t.Fatal("phone still paired")
	}
}
