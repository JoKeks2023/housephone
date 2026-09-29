package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExampleConfigParsesAndValidates(t *testing.T) {
	cfg, err := Load("../../config.example.yaml", false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SIP.Password = "secret"
	if err := cfg.ValidateServe(); err != nil {
		t.Fatalf("example config invalid: %v", err)
	}
	if err := cfg.ValidatePair(); err != nil {
		t.Fatalf("example config cannot pair: %v", err)
	}
}

func TestLoadAppliesDefaultsAndYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "config.yaml", `
bridge:
  name: Ferienhaus
sip:
  username: "621"
media:
  udpPort: 51000
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bridge.Name != "Ferienhaus" || cfg.SIP.Username != "621" || cfg.Media.UDPPort != 51000 {
		t.Fatalf("yaml not applied: %+v", cfg)
	}
	if cfg.SIP.Registrar != "fritz.box" || cfg.SIP.BindPort != 5062 || cfg.Bridge.Listen != ":8080" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestMissingConfigAllowed(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), false); err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	pwFile := writeFile(t, dir, "sip_password", "from-file\n")
	env := map[string]string{
		EnvSIPPasswordFile: pwFile,
		EnvAPNsKeyFile:     "/secrets/AuthKey.p8",
		EnvAPNsKeyID:       "ABC123",
		EnvPublicURL:       "wss://phone.example.com/v1/ws",
		EnvDataDir:         "/data",
	}
	cfg := Default()
	if err := cfg.applyEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok }); err != nil {
		t.Fatal(err)
	}
	if cfg.SIP.Password != "from-file" {
		t.Fatalf("password file not applied: %q", cfg.SIP.Password)
	}
	if cfg.APNs.KeyFile != "/secrets/AuthKey.p8" || cfg.APNs.KeyID != "ABC123" || !cfg.APNs.Enabled() {
		t.Fatalf("apns env not applied: %+v", cfg.APNs)
	}
	if cfg.Bridge.PublicURL != "wss://phone.example.com/v1/ws" || cfg.Bridge.DataDir != "/data" {
		t.Fatalf("bridge env not applied: %+v", cfg.Bridge)
	}
}

func TestValidateServeReportsProblems(t *testing.T) {
	cfg := Default()
	cfg.Media.PublicIP = "300.1.1.1"
	cfg.APNs.KeyFile = "/x.p8"
	cfg.Bridge.PublicURL = "https://example.com"
	err := cfg.ValidateServe()
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{"sip.username", "sip.password", "media.publicIp", "apns", "wss://"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestFritzBoxConfig(t *testing.T) {
	cfg := Default()
	if cfg.FritzBox.Enabled() || cfg.FritzBox.Timezone != "Europe/Berlin" || cfg.FritzBox.CountryCode != "49" {
		t.Fatalf("defaults: %+v", cfg.FritzBox)
	}
	if cfg.FritzBoxHost() != "fritz.box" {
		t.Fatalf("host should default to sip.registrar, got %q", cfg.FritzBoxHost())
	}
	cfg.FritzBox.Host = "192.168.0.1"
	if cfg.FritzBoxHost() != "192.168.0.1" {
		t.Fatalf("explicit host ignored: %q", cfg.FritzBoxHost())
	}

	dir := t.TempDir()
	env := map[string]string{EnvFritzBoxPasswordFile: writeFile(t, dir, "fritzbox_password", "geheim\n")}
	cfg = Default()
	if err := cfg.applyEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok }); err != nil {
		t.Fatal(err)
	}
	if cfg.FritzBox.Password != "geheim" {
		t.Fatalf("password file not applied: %q", cfg.FritzBox.Password)
	}
	env = map[string]string{EnvFritzBoxPassword: "direkt"}
	cfg = Default()
	_ = cfg.applyEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if cfg.FritzBox.Password != "direkt" {
		t.Fatalf("password env not applied: %q", cfg.FritzBox.Password)
	}
}

func TestFritzBoxValidation(t *testing.T) {
	valid := Default()
	valid.SIP.Username, valid.SIP.Password = "620", "x"
	valid.FritzBox.Username, valid.FritzBox.Password = "housephone", "geheim"
	if err := valid.ValidateServe(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	broken := valid
	broken.FritzBox.Password = ""
	broken.FritzBox.Timezone = "Mars/Olympus"
	broken.FritzBox.CountryCode = "+49"
	err := broken.ValidateServe()
	for _, want := range []string{"fritzbox.password", "fritzbox.timezone", "fritzbox.countryCode"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}

	// Without username, fritzbox.* is not checked (feature off).
	off := valid
	off.FritzBox = FritzBox{Timezone: "nonsense"}
	if err := off.ValidateServe(); err != nil {
		t.Fatalf("disabled fritzbox section validated: %v", err)
	}
}

func TestIsIPv4(t *testing.T) {
	for ip, want := range map[string]bool{
		"1.2.3.4": true, "192.168.178.1": true, "01.2.3.4": false,
		"1.2.3": false, "::1": false, "a.b.c.d": false,
	} {
		if got := isIPv4(ip); got != want {
			t.Errorf("isIPv4(%q) = %v, want %v", ip, got, want)
		}
	}
}
