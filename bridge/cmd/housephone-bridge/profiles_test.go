package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// ADR-0008: profiles in the CLI.

func profileConfig(dataDir string) config.Config {
	cfg := config.Default()
	cfg.Bridge.DataDir = dataDir
	cfg.SIP.Username = "620"
	cfg.Profile.Name = "Profil A"
	cfg.Lines = []config.Line{{ID: "b", Name: "Profil B", SIP: config.LineSIP{Username: "621", Password: "x"}, Numbers: []string{"030 1234568"}}}
	return cfg
}

func TestMoveDeviceOfflineTakesTheWatchAlong(t *testing.T) {
	reg, _ := seedDevices(t)
	cfg := profileConfig(t.TempDir())
	var out bytes.Buffer
	if err := moveDevice(cfg, reg, nil, []string{"phone", "b"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"phone", "watch"} {
		d, err := reg.Get(id)
		if err != nil || d.Profile != "b" {
			t.Fatalf("%s: profile %q %v", id, d.Profile, err)
		}
	}
	if other, _ := reg.Get("other"); other.Profile != "" {
		t.Fatalf("an unrelated device moved: %q", other.Profile)
	}
	if !strings.Contains(out.String(), "Profil B") {
		t.Fatalf("output:\n%s", out.String())
	}
	// Back to the default profile: stored as "" like before profiles.
	if err := moveDevice(cfg, reg, nil, []string{"phone", "default"}, &out); err != nil {
		t.Fatal(err)
	}
	if d, _ := reg.Get("phone"); d.Profile != "" {
		t.Fatalf("default profile stored as %q", d.Profile)
	}
	if err := moveDevice(cfg, reg, nil, []string{"phone", "nope"}, &out); err == nil || !strings.Contains(err.Error(), "unbekanntes Profil") {
		t.Fatalf("unknown profile: %v", err)
	}
}

func TestPairRejectsAnUnknownProfile(t *testing.T) {
	cfg := profileConfig(t.TempDir())
	cfg.Bridge.PublicURL = "wss://phone.example.com/v1/ws"
	err := pair(context.Background(), cfg, "", "nope", &bytes.Buffer{}, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "unbekanntes Profil") {
		t.Fatalf("pair -profile nope: %v", err)
	}
	if codes, _ := store.NewPairing(cfg.Bridge.DataDir).Pending(time.Now()); len(codes) != 0 {
		t.Fatalf("a code was created: %+v", codes)
	}
}

func TestProfilesCommandWithoutBridge(t *testing.T) {
	var out bytes.Buffer
	if err := profiles(profileConfig(t.TempDir()), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"default", "Profil A", "– (keine Anrufliste)", "Profil B", "030 1234568"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("profiles misses %q:\n%s", want, out.String())
		}
	}
}

func TestLanApprovalAsksForTheProfile(t *testing.T) {
	svc := &lanService{
		requests: []admin.LanPairingRequest{{ID: "Abc123xyz", DeviceName: "iPhone B", IP: "192.168.0.30", SAS: "123456", ExpiresAt: time.Now().Add(time.Minute)}},
		profiles: []admin.ProfileInfo{{ID: "default", Name: "Profil A"}, {ID: "b", Name: "Profil B"}},
	}
	c := lanClient(t, svc)
	var out bytes.Buffer
	if err := lanPairing(c, []string{"approve", "Abc123xyz"}, strings.NewReader("j\n2\n"), &out); err != nil {
		t.Fatal(err)
	}
	if len(svc.approved) != 1 || svc.approved[0] != "Abc123xyz/b" {
		t.Fatalf("approved %v\n%s", svc.approved, out.String())
	}
	if !strings.Contains(out.String(), "Zu welchem Profil gehört das Gerät?") {
		t.Fatalf("no question:\n%s", out.String())
	}
	// An invalid choice approves nothing.
	if err := lanPairing(c, []string{"approve", "Abc123xyz"}, strings.NewReader("j\n7\n"), &out); err == nil || len(svc.approved) != 1 {
		t.Fatalf("invalid choice: %v %v", err, svc.approved)
	}
	// -profile skips the question.
	if err := lanPairing(c, []string{"approve", "-profile", "b", "-code", "123456", "Abc123xyz"}, nil, &out); err != nil || svc.approved[1] != "Abc123xyz/b" {
		t.Fatalf("approve -profile: %v %v", err, svc.approved)
	}
}
