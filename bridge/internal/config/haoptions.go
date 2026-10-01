package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Home Assistant add-on mode: the Supervisor writes the options the user
// set in the add-on UI to /data/options.json. They replace config.yaml.
const (
	// HAOptionsPath is where the Supervisor puts the add-on options.
	HAOptionsPath = "/data/options.json"
	// HAGateway is the host's address on the Supervisor network
	// (hassio, 172.30.32.0/23). A host-network add-on is reached there by
	// the ingress proxy and by other add-ons such as Cloudflared.
	HAGateway = "172.30.32.1"
	// HAIngressProxy is the Supervisor's ingress proxy; the dashboard only
	// accepts connections from it.
	HAIngressProxy = "172.30.32.2"
	// HAIngressPort is the add-on's ingress_port (8099, the default).
	HAIngressPort = 8099
	// HANetwork is the Supervisor network. Other add-ons live there.
	HANetwork = "172.30.32.0/23"
)

// HAOptions mirrors the add-on schema (ha-addon/housephone-bridge/config.yaml).
type HAOptions struct {
	BridgeName       string `json:"bridge_name"`
	PublicURL        string `json:"public_url"`
	LanURL           string `json:"lan_url"`
	TunnelPort       int    `json:"tunnel_port"`
	Tailscale        bool   `json:"tailscale"`
	Bonjour          *bool  `json:"bonjour"`
	FritzBoxHost     string `json:"fritzbox_host"`
	SIPUsername      string `json:"sip_username"`
	SIPPassword      string `json:"sip_password"`
	FritzBoxUsername string `json:"fritzbox_username"`
	FritzBoxPassword string `json:"fritzbox_password"`
	APNsKeyFile      string `json:"apns_key_file"`
	APNsKeyID        string `json:"apns_key_id"`
	APNsTeamID       string `json:"apns_team_id"`
	APNsTopic        string `json:"apns_topic"`
	LogLevel         string `json:"log_level"`
}

// LoadHAOptions builds the configuration from the add-on options at path.
func LoadHAOptions(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read add-on options: %w", err)
	}
	var o HAOptions
	if err := json.Unmarshal(data, &o); err != nil {
		// No %w: the decoder error could quote option values (secrets).
		return Config{}, fmt.Errorf("parse add-on options %s: invalid JSON", path)
	}
	return o.Config(), nil
}

// Config maps the options onto the defaults.
//
// Listeners in the add-on (host network):
//   - public (tunnel): HAGateway:tunnel_port, reachable only from the host
//     and the add-on network, where the Cloudflared add-on runs. Proxy
//     headers are believed from there.
//   - private (home network): :8081 as usual, but the add-on network is
//     excluded, so no add-on (and no tunnel pointed at 8081) can pair.
func (o HAOptions) Config() Config {
	c := Default()
	c.Bridge.DataDir = "/data"
	port := o.TunnelPort
	if port == 0 {
		port = 8080
	}
	c.Bridge.Listen = fmt.Sprintf("%s:%d", HAGateway, port)
	c.Bridge.TrustProxyHeaders = true
	c.Bridge.TrustedProxies = []string{HANetwork}
	c.Bridge.ExcludedNetworks = []string{HANetwork}
	c.Bridge.Tailscale = o.Tailscale
	c.Bridge.Bonjour = o.Bonjour
	setIf(&c.Bridge.Name, o.BridgeName)
	c.Bridge.PublicURL = strings.TrimSpace(o.PublicURL)
	c.Bridge.LanURL = strings.TrimSpace(o.LanURL)
	setIf(&c.SIP.Registrar, o.FritzBoxHost)
	c.SIP.Username = strings.TrimSpace(o.SIPUsername)
	c.SIP.Password = o.SIPPassword
	c.FritzBox.Username = strings.TrimSpace(o.FritzBoxUsername)
	c.FritzBox.Password = o.FritzBoxPassword
	c.APNs.KeyFile = strings.TrimSpace(o.APNsKeyFile)
	c.APNs.KeyID = strings.TrimSpace(o.APNsKeyID)
	setIf(&c.APNs.TeamID, o.APNsTeamID)
	setIf(&c.APNs.Topic, o.APNsTopic)
	setIf(&c.Log.Level, o.LogLevel)
	return c
}

func setIf(dst *string, v string) {
	if v = strings.TrimSpace(v); v != "" {
		*dst = v
	}
}
