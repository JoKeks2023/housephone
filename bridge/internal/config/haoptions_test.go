package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeOptions(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "options.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadHAOptions(t *testing.T) {
	path := writeOptions(t, `{
		"bridge_name": "Zuhause",
		"public_url": "wss://phone.example.com/v1/ws",
		"lan_url": "",
		"tunnel_port": 8080,
		"tailscale": true,
		"fritzbox_host": "192.168.178.1",
		"sip_username": " 620 ",
		"sip_password": "s3cret",
		"fritzbox_username": "housephone",
		"fritzbox_password": "fb-secret",
		"apns_key_file": "/config/AuthKey.p8",
		"apns_key_id": "ABC123",
		"apns_team_id": "",
		"apns_topic": "",
		"log_level": "debug"
	}`)
	c, err := LoadHAOptions(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Bridge.Listen != "172.30.32.1:8080" {
		t.Errorf("listen = %q", c.Bridge.Listen)
	}
	if c.Bridge.PrivateListen != ":8081" || c.Bridge.DataDir != "/data" {
		t.Errorf("privateListen/dataDir = %q/%q", c.Bridge.PrivateListen, c.Bridge.DataDir)
	}
	if c.SIP.Registrar != "192.168.178.1" || c.SIP.Username != "620" || c.SIP.Password != "s3cret" {
		t.Errorf("sip = %+v", c.SIP)
	}
	if !c.FritzBox.Enabled() || c.FritzBox.Password != "fb-secret" {
		t.Errorf("fritzbox = %+v", c.FritzBox)
	}
	// Empty team/topic keep the defaults.
	if !c.APNs.Enabled() || c.APNs.TeamID != Default().APNs.TeamID {
		t.Errorf("apns = %+v", c.APNs)
	}
	if !c.Bridge.Tailscale || c.Log.Level != "debug" {
		t.Errorf("tailscale/log = %v/%q", c.Bridge.Tailscale, c.Log.Level)
	}
	if err := c.ValidateServe(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestHAOptionsExcludeAddonNetwork(t *testing.T) {
	c := HAOptions{}.Config()
	excluded, err := c.Bridge.ExcludedNetworkNets()
	if err != nil || len(excluded) != 1 {
		t.Fatalf("excluded = %v, %v", excluded, err)
	}
	trusted, _ := c.Bridge.TrustedNetworkNets()
	// The add-on network lies inside 172.16.0.0/12, so the exclusion matters.
	var inTrusted bool
	for _, n := range trusted {
		if n.Contains(excluded[0].IP) {
			inTrusted = true
		}
	}
	if !inTrusted || !excluded[0].Contains([]byte{172, 30, 33, 7}) {
		t.Errorf("add-on network not covered: %v", excluded)
	}
	if c.Bridge.Listen != "172.30.32.1:8080" {
		t.Errorf("default tunnel listen = %q", c.Bridge.Listen)
	}
}

func TestLoadHAOptionsDoesNotLeakSecrets(t *testing.T) {
	path := writeOptions(t, `{"sip_password": "leak-me", `)
	_, err := LoadHAOptions(path)
	if err == nil || strings.Contains(err.Error(), "leak-me") {
		t.Fatalf("err = %v", err)
	}
}
