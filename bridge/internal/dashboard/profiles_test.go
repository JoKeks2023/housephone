package dashboard

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

// ADR-0008: with several profiles the dashboard shows them and asks whose
// device it is.

func withProfiles(svc *fakeService) {
	svc.profiles = []admin.ProfileInfo{
		{ID: "default", Name: "Profil A", SIPUser: "620", Numbers: []string{"030 1234567"}, Registered: true, HistoryAllowed: true, Devices: 2, DevicesOnline: 1},
		{ID: "b", Name: "Profil B", SIPUser: "621", Devices: 0},
	}
	for i := range svc.devices {
		svc.devices[i].Profile, svc.devices[i].ProfileName = "default", "Profil A"
	}
}

func TestSingleProfileHidesProfileChoices(t *testing.T) {
	_, h, _ := setup(t)
	body := do(h, "GET", "/", proxy, nil, nil).Body.String()
	if strings.Contains(body, `name="profile"`) || strings.Contains(body, "devices/iphone/move") {
		t.Fatal("profile choices without profiles")
	}
}

func TestProfilesOnTheOverview(t *testing.T) {
	svc, h, _ := setup(t)
	withProfiles(svc)
	svc.lan = []admin.LanPairingRequest{{ID: "r1", DeviceName: "iPhone B", SAS: "123456", ExpiresAt: time.Now().Add(time.Minute)}}
	body := do(h, "GET", "/", proxy, nil, map[string]string{"Accept-Language": "de"}).Body.String()
	for _, want := range []string{
		"Profil A", "Profil B", "030 1234567", "keine eigene Nummer",
		`action="devices/iphone/move"`, `<option value="b">Profil B</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview misses %q", want)
		}
	}
	// A watch follows its iPhone: no move of its own.
	if strings.Contains(body, "devices/watch/move") {
		t.Error("watch can be moved on its own")
	}
	// Pairing form and LAN approval both carry the profile choice.
	if strings.Count(body, `<select name="profile"`) < 3 {
		t.Errorf("profile selects: %d", strings.Count(body, `<select name="profile"`))
	}
}

func TestProfileActions(t *testing.T) {
	svc, h, _ := setup(t)
	withProfiles(svc)
	svc.lan = []admin.LanPairingRequest{{ID: "r1", DeviceName: "iPhone B", SAS: "123456", ExpiresAt: time.Now().Add(time.Minute)}}
	token := csrfOf(t, h)

	if w := do(h, "POST", "/devices/iphone/move", proxy, url.Values{"csrf": {token}, "profile": {"b"}}, nil); w.Code != http.StatusSeeOther {
		t.Fatalf("move: %d", w.Code)
	}
	if len(svc.moved) != 1 || svc.moved[0] != "iphone/b" {
		t.Fatalf("moved %v", svc.moved)
	}
	if w := do(h, "POST", "/devices/iphone/move", proxy, url.Values{"csrf": {token}, "profile": {"nope"}}, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("move to an unknown profile: %d", w.Code)
	}
	if w := do(h, "POST", "/devices/iphone/move", proxy, url.Values{"profile": {"b"}}, nil); w.Code != http.StatusForbidden || len(svc.moved) != 1 {
		t.Fatalf("move without csrf: %d", w.Code)
	}
	if w := do(h, "POST", "/lan/r1/approve", proxy, url.Values{"csrf": {token}, "profile": {"b"}}, nil); w.Code != http.StatusSeeOther {
		t.Fatalf("approve: %d", w.Code)
	}
	if len(svc.approved) != 1 || svc.approved[0] != "r1/b" {
		t.Fatalf("approved %v", svc.approved)
	}
	if w := do(h, "POST", "/pair", proxy, url.Values{"csrf": {token}, "profile": {"b"}, "name": {"iPhone B"}}, nil); w.Code != http.StatusOK {
		t.Fatalf("pair: %d", w.Code)
	}
	if len(svc.pairings) != 1 || svc.pairings[0] != "iPhone B/b" {
		t.Fatalf("pairings %v", svc.pairings)
	}
}
