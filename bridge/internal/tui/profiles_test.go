package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

// ADR-0008: with several profiles the TUI asks whose device it is.

func twoProfiles() []admin.ProfileInfo {
	return []admin.ProfileInfo{
		{ID: "default", Name: "Profil A", SIPUser: "620", Numbers: []string{"030 1234567"}, Registered: true, HistoryAllowed: true, Devices: 2, DevicesOnline: 1},
		{ID: "b", Name: "Profil B", SIPUser: "621", Registered: false, Devices: 0},
	}
}

func TestOverviewListsProfiles(t *testing.T) {
	m := newTest(t, &fakeAPI{profiles: twoProfiles()})
	v := plain(m.View())
	for _, want := range []string{"Profil A · 620", "030 1234567", "Profil B · 621 nicht angemeldet", "keine eigene Nummer"} {
		if !strings.Contains(v, want) {
			t.Fatalf("overview misses %q:\n%s", want, v)
		}
	}
}

func TestPairingAsksForTheProfile(t *testing.T) {
	api := &fakeAPI{profiles: twoProfiles()}
	m := newTest(t, api)
	m = press(t, m, "3", "n")
	m = press(t, m, "enter") // empty name
	if v := plain(m.View()); !strings.Contains(v, "Für welches Profil ist das neue Gerät?") {
		t.Fatalf("no profile choice:\n%s", v)
	}
	m = press(t, m, "down", "enter")
	if len(api.paired) != 1 || api.paired[0] != "/b" {
		t.Fatalf("paired %v", api.paired)
	}
	if v := plain(m.View()); !strings.Contains(v, "Profil b") {
		t.Fatalf("code view misses the profile:\n%s", v)
	}
}

func TestLanApprovalAsksForTheProfile(t *testing.T) {
	api := &fakeAPI{profiles: twoProfiles(), lan: []admin.LanPairingRequest{
		{ID: "r1", DeviceName: "iPhone B", IP: "192.168.0.30", SAS: "123456", ExpiresAt: t0.Add(2 * time.Minute)},
	}}
	m := newTest(t, api)
	m = press(t, m, "3", "a", "j")
	if len(api.approved) != 0 {
		t.Fatal("approved before the profile was chosen")
	}
	if v := plain(m.View()); !strings.Contains(v, "Zu welchem Profil gehört das Gerät?") {
		t.Fatalf("no profile choice:\n%s", v)
	}
	m = press(t, m, "esc")
	if len(api.approved) != 0 {
		t.Fatal("Esc approved")
	}
	m = press(t, m, "a", "j", "down", "enter")
	if len(api.approved) != 1 || api.approved[0] != "r1/b" {
		t.Fatalf("approved %v", api.approved)
	}
}

func TestMoveDevice(t *testing.T) {
	api := &fakeAPI{profiles: twoProfiles()}
	m := newTest(t, api)
	m = press(t, m, "2")
	if v := plain(m.View()); !strings.Contains(v, "PROFIL") {
		t.Fatalf("devices without profile column:\n%s", v)
	}
	m = press(t, m, "p", "down", "enter")
	if len(api.moved) != 1 || api.moved[0] != "i1/b" {
		t.Fatalf("moved %v", api.moved)
	}
	// A watch follows its iPhone.
	m = press(t, m, "down", "p")
	if v := plain(m.View()); !strings.Contains(v, "Eine Uhr gehört immer zum Profil ihres iPhones") || m.picker != nil {
		t.Fatalf("watch could be moved on its own:\n%s", v)
	}
}

func TestSingleProfileAsksNothing(t *testing.T) {
	api := &fakeAPI{lan: []admin.LanPairingRequest{{ID: "r1", DeviceName: "iPhone", SAS: "123456", ExpiresAt: t0.Add(time.Minute)}}}
	m := newTest(t, api)
	m = press(t, m, "3", "a", "j")
	if len(api.approved) != 1 || api.approved[0] != "r1" {
		t.Fatalf("approved %v", api.approved)
	}
	m = press(t, m, "n", "enter")
	if len(api.paired) != 1 || api.paired[0] != "/" {
		t.Fatalf("paired %v", api.paired)
	}
	m = press(t, m, "2", "p")
	if m.picker != nil {
		t.Fatal("move offered without profiles")
	}
}
