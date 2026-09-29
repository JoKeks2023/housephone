package app

import (
	"context"
	"fmt"
	"net"

	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/lan"
)

// LanURL returns the ws:// URL of the private listener that devices use at
// home and for pairing: bridge.lanUrl, or built from lanIP (the bridge's
// address in the home network) and the port of bridge.privateListen. A
// listener bound to one address uses that address instead of lanIP. Empty
// without a private listener.
func LanURL(cfg config.Config, lanIP string) string {
	if cfg.Bridge.LanURL != "" {
		return cfg.Bridge.LanURL
	}
	if cfg.Bridge.PrivateListen == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(cfg.Bridge.PrivateListen)
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = lanIP
	}
	if host == "" {
		return ""
	}
	return "ws://" + net.JoinHostPort(host, port) + "/v1/ws"
}

// DetectLanIP returns the bridge's address in the home network:
// sip.bindHost, or the local address that routes to the FRITZ!Box (the same
// choice the SIP leg makes).
func DetectLanIP(ctx context.Context, cfg config.Config) (string, error) {
	if cfg.SIP.BindHost != "" {
		return cfg.SIP.BindHost, nil
	}
	registrar, err := lan.Resolve(ctx, cfg.SIP.Registrar)
	if err != nil {
		return "", fmt.Errorf("sip.registrar: %w", err)
	}
	return lan.RouteIP(registrar.String(), cfg.SIP.Port)
}

// PairingLanURL is LanURL for the pair command, which runs without the SIP
// leg. It fails when no address can be found, because devices can only
// pair over the private listener.
func PairingLanURL(ctx context.Context, cfg config.Config) (string, error) {
	if cfg.Bridge.LanURL != "" {
		return cfg.Bridge.LanURL, nil
	}
	ip, err := DetectLanIP(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("LAN-Adresse der Bridge nicht gefunden (%w) – trag bridge.lanUrl ein, z. B. ws://192.168.178.20:8081/v1/ws", err)
	}
	u := LanURL(cfg, ip)
	if u == "" {
		return "", fmt.Errorf("bridge.privateListen fehlt – Geräte koppeln nur im Heimnetz")
	}
	return u, nil
}
