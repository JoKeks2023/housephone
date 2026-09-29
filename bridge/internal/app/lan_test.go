package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
)

// Security review N1: with a public resolver "fritz.box" points to a third
// party. The bridge must refuse to start instead of sending its logins there.
func TestNewRejectsFritzBoxOutsideHomeNetwork(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*config.Config)
		want string
	}{
		{"registrar", func(c *config.Config) { c.SIP.Registrar = "212.42.244.122" }, "sip.registrar"},
		{"tr064 host", func(c *config.Config) {
			c.SIP.Registrar = "127.0.0.1"
			c.FritzBox.Username = "housephone"
			c.FritzBox.Password = "x"
			c.FritzBox.Host = "212.42.244.122"
		}, "fritzbox.host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Bridge.Listen = "127.0.0.1:0"
			cfg.Bridge.DataDir = t.TempDir()
			cfg.SIP.Username, cfg.SIP.Password = "620", "geheim"
			cfg.SIP.BindHost = "127.0.0.1"
			cfg.Media.DetectPublicIP = false
			tc.edit(&cfg)
			_, err := app.New(context.Background(), cfg, logger())
			if err == nil {
				t.Fatal("bridge started with a FRITZ!Box address outside the home network")
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Heimnetz") {
				t.Fatalf("error %q", err)
			}
		})
	}
}
