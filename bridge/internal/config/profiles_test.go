package config

import (
	"strings"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/profile"
)

func TestProfilesFromYAML(t *testing.T) {
	dir := t.TempDir()
	secret := writeFile(t, dir, "line_b", "geheim-b\n")
	path := writeFile(t, dir, "config.yaml", `
sip:
  username: housephone-a
  password: geheim-a
profile:
  name: Profil A
  numbers: ["030 1234567"]
lines:
  - id: b
    name: Profil B
    sip:
      username: housephone-b
      passwordFile: `+secret+`
    numbers: ["030 1234568"]
    phonebooks: [1, 2]
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateServe(); err != nil {
		t.Fatalf("valid profiles rejected: %v", err)
	}
	if cfg.Lines[0].SIP.Password != "geheim-b" {
		t.Fatalf("passwordFile not read: %q", cfg.Lines[0].SIP.Password)
	}
	if got := cfg.LineBindPort(0); got != cfg.SIP.BindPort+1 {
		t.Fatalf("default line port = %d", got)
	}
	set := cfg.Profiles()
	if !set.Multi() || len(set.List()) != 2 {
		t.Fatalf("profiles = %+v", set.List())
	}
	a, _ := set.Get("")
	b, _ := set.Get("b")
	if a.Name != "Profil A" || a.SIPUser != "housephone-a" || a.Number() != "030 1234567" {
		t.Fatalf("default profile = %+v", a)
	}
	if b.Name != "Profil B" || b.SIPUser != "housephone-b" || strings.Join(b.Phonebooks, ",") != "1,2" {
		t.Fatalf("line profile = %+v", b)
	}
}

func TestSingleProfileKeepsOldConfigs(t *testing.T) {
	cfg := Default()
	cfg.SIP.Username, cfg.SIP.Password = "620", "x"
	if err := cfg.ValidateServe(); err != nil {
		t.Fatal(err)
	}
	set := cfg.Profiles()
	p, ok := set.Get("")
	if set.Multi() || !ok || p.ID != profile.DefaultID || p.Name != DefaultProfileName {
		t.Fatalf("old config profiles = %+v", set.List())
	}
}

func TestProfileValidation(t *testing.T) {
	base := Default()
	base.SIP.Username, base.SIP.Password = "620", "x"

	bad := base
	bad.Profile.Numbers = []string{"030 abc"}
	bad.Lines = []Line{
		{ID: "default", Name: "X", SIP: LineSIP{Username: "621", Password: "x"}},
		{ID: "Mama!", Name: "", SIP: LineSIP{Username: "620", Password: ""}},
		{ID: "c", Name: "C", SIP: LineSIP{Username: "622", Password: "x", BindPort: base.SIP.BindPort}, Phonebooks: []string{"eins"}},
	}
	err := bad.ValidateServe()
	for _, want := range []string{
		`profile.numbers: "030 abc"`,
		`lines[0].id "default" is used twice`,
		"lines[1].id must match",
		"lines[1].name is required",
		`lines[1].sip.username "620" is used twice`,
		"lines[1].sip.password or passwordFile is required",
		"lines[2].sip.bindPort 5062 is already used by sip.bindPort",
		`lines[2].phonebooks: "eins"`,
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}

	many := base
	for i := range profile.MaxProfiles {
		many.Lines = append(many.Lines, Line{ID: string(rune('a' + i)), Name: "X", SIP: LineSIP{Username: string(rune('a' + i)), Password: "x"}})
	}
	if err := many.ValidateServe(); err == nil || !strings.Contains(err.Error(), "at most 8 profiles") {
		t.Fatalf("too many profiles accepted: %v", err)
	}
}

func TestHAOptionsLines(t *testing.T) {
	o := HAOptions{
		SIPUsername: "a", SIPPassword: "pa",
		ProfileName: "Profil A", ProfileNumbers: "030 1234567, ",
		Lines: []HALineOptions{{ID: "b", Name: "Profil B", SIPUsername: "b", SIPPassword: "pb", Numbers: "030 1234568,030 1234569"}},
	}
	c := o.Config()
	if c.Profile.Name != "Profil A" || len(c.Profile.Numbers) != 1 {
		t.Fatalf("default profile = %+v", c.Profile)
	}
	if len(c.Lines) != 1 || c.Lines[0].SIP.Password != "pb" || len(c.Lines[0].Numbers) != 2 {
		t.Fatalf("lines = %+v", c.Lines)
	}
}
