// Package config loads the bridge configuration from YAML with environment
// overrides for secrets.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	// fritzbox.timezone is validated in the Docker image without zoneinfo.
	_ "time/tzdata"

	"gopkg.in/yaml.v3"
)

// Environment variables that override configuration values.
const (
	EnvConfig          = "HOUSEPHONE_CONFIG"
	EnvDataDir         = "HOUSEPHONE_DATA_DIR"
	EnvPublicURL       = "HOUSEPHONE_PUBLIC_URL"
	EnvLanURL          = "HOUSEPHONE_LAN_URL"
	EnvSIPPassword     = "HOUSEPHONE_SIP_PASSWORD"
	EnvSIPPasswordFile = "HOUSEPHONE_SIP_PASSWORD_FILE"
	EnvAPNsKeyFile     = "HOUSEPHONE_APNS_KEY_FILE"
	EnvAPNsKeyID       = "HOUSEPHONE_APNS_KEY_ID"
	EnvAPNsTeamID      = "HOUSEPHONE_APNS_TEAM_ID"
	EnvPublicIP        = "HOUSEPHONE_PUBLIC_IP"
	EnvLogLevel        = "HOUSEPHONE_LOG_LEVEL"

	EnvFritzBoxPassword     = "HOUSEPHONE_FRITZBOX_PASSWORD"
	EnvFritzBoxPasswordFile = "HOUSEPHONE_FRITZBOX_PASSWORD_FILE"
)

// DefaultConfigPath is used when neither a flag nor HOUSEPHONE_CONFIG is set.
const DefaultConfigPath = "config.yaml"

type Config struct {
	Bridge   Bridge   `yaml:"bridge"`
	SIP      SIP      `yaml:"sip"`
	Media    Media    `yaml:"media"`
	APNs     APNs     `yaml:"apns"`
	FritzBox FritzBox `yaml:"fritzbox"`
	Log      Log      `yaml:"log"`
}

// FritzBox configures TR-064 access for the phonebook and call list
// (signaling v1.2). It needs a FRITZ!Box user with the right "Sprachnachrichten,
// Faxnachrichten, FRITZ!App Fon und Anrufliste".
type FritzBox struct {
	// Host of the FRITZ!Box. Empty: sip.registrar.
	Host string `yaml:"host"`
	// Port is the unencrypted TR-064 port, only used to ask for the TLS
	// port (49000 on every FRITZ!Box).
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// Timezone of the FRITZ!Box; the call list uses local time.
	Timezone string `yaml:"timezone"`
	// CountryCode without "+", for matching caller numbers ("49").
	CountryCode string `yaml:"countryCode"`
}

// Enabled reports whether phonebook and call list are configured.
func (f FritzBox) Enabled() bool { return f.Username != "" }

type Bridge struct {
	// Name is shown in the app, e.g. "Zuhause".
	Name string `yaml:"name"`
	// Listen is the public HTTP/WebSocket listen address behind the
	// Cloudflare Tunnel, e.g. "127.0.0.1:8080". It only serves telephony
	// for paired devices: no pairing, no administration.
	Listen string `yaml:"listen"`
	// PrivateListen is the listener for the home network (and Tailscale):
	// everything Listen serves plus pairing. It only accepts connections
	// from TrustedNetworks; loopback never counts (cloudflared connects from
	// there). Empty disables it, and with it pairing.
	PrivateListen string `yaml:"privateListen"`
	// TrustedNetworks are the source CIDRs PrivateListen accepts. Empty:
	// RFC 1918, unique local (fc00::/7) and link-local addresses.
	TrustedNetworks []string `yaml:"trustedNetworks"`
	// Tailscale adds the Tailscale ranges (100.64.0.0/10,
	// fd7a:115c:a1e0::/48) to TrustedNetworks.
	Tailscale bool `yaml:"tailscale"`
	// LanURL is the ws:// URL of PrivateListen that devices use at home and
	// for pairing. Empty: derived from the bridge's LAN IP.
	LanURL string `yaml:"lanUrl"`
	// PublicURL is the wss:// URL devices use, embedded in pairing links.
	PublicURL string `yaml:"publicUrl"`
	// DataDir holds devices.json, pairing.json and bridge.json.
	DataDir string `yaml:"dataDir"`
	// TrustProxyHeaders uses CF-Connecting-IP / X-Forwarded-For as client IP.
	// Enable only behind Cloudflare Tunnel or a reverse proxy. The headers
	// are only believed from loopback and from TrustedProxies.
	TrustProxyHeaders bool `yaml:"trustProxyHeaders"`
	// TrustedProxies are extra proxy addresses or CIDRs (e.g. a reverse
	// proxy on another host) whose proxy headers are believed.
	TrustedProxies []string `yaml:"trustedProxies"`
	// MaxCalls bounds concurrent calls; devices cannot dial beyond it.
	MaxCalls int `yaml:"maxCalls"`
}

// DefaultTrustedNetworks are the home network ranges: RFC 1918, unique
// local and link-local. Loopback is deliberately missing.
var DefaultTrustedNetworks = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"fc00::/7", "169.254.0.0/16", "fe80::/10",
}

// TailscaleNetworks are the address ranges Tailscale assigns.
var TailscaleNetworks = []string{"100.64.0.0/10", "fd7a:115c:a1e0::/48"}

// TrustedProxyNets parses TrustedProxies (IP addresses or CIDRs).
func (b Bridge) TrustedProxyNets() ([]*net.IPNet, error) {
	return parseNets("bridge.trustedProxies", b.TrustedProxies)
}

// TrustedNetworkNets returns the source networks the private listener
// accepts: TrustedNetworks (or the defaults) plus Tailscale if enabled.
func (b Bridge) TrustedNetworkNets() ([]*net.IPNet, error) {
	raw := b.TrustedNetworks
	if len(raw) == 0 {
		raw = DefaultTrustedNetworks
	}
	if b.Tailscale {
		raw = append(append([]string{}, raw...), TailscaleNetworks...)
	}
	return parseNets("bridge.trustedNetworks", raw)
}

// parseNets parses IP addresses or CIDRs.
func parseNets(field string, list []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return nil, fmt.Errorf("%s: %q is no IP address or CIDR", field, raw)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			raw = fmt.Sprintf("%s/%d", ip, bits)
		}
		_, n, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is no IP address or CIDR", field, raw)
		}
		out = append(out, n)
	}
	return out, nil
}

type SIP struct {
	// Registrar is the FRITZ!Box host, usually "fritz.box" or its LAN IP.
	Registrar string `yaml:"registrar"`
	Port      int    `yaml:"port"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
	// BindHost is the local IP for SIP and RTP. Empty: the IP that routes to
	// the registrar.
	BindHost string `yaml:"bindHost"`
	BindPort int    `yaml:"bindPort"`
	// RTPPortMin/Max bound the RTP ports used towards the FRITZ!Box.
	RTPPortMin int `yaml:"rtpPortMin"`
	RTPPortMax int `yaml:"rtpPortMax"`
	// RegisterExpirySeconds is the requested registration lifetime.
	RegisterExpirySeconds int `yaml:"registerExpirySeconds"`
}

type Media struct {
	// UDPPort is the single UDP port for all WebRTC media (forward it on the
	// FRITZ!Box to this server).
	UDPPort int `yaml:"udpPort"`
	// PublicIP is the public IPv4 address. Takes precedence over PublicHost.
	PublicIP string `yaml:"publicIp"`
	// PublicHost is resolved periodically (e.g. a MyFRITZ! or DynDNS name).
	PublicHost string `yaml:"publicHost"`
	// DetectPublicIP finds the public IP automatically when neither PublicIP
	// nor PublicHost is set: from the FRITZ!Box (see PublicIPFromRouter),
	// otherwise via STUN.
	DetectPublicIP bool `yaml:"detectPublicIp"`
	// PublicIPFromRouter asks the FRITZ!Box at sip.registrar for its external
	// IPv4 via UPnP every 30 s, so a changing IP is noticed within seconds.
	// STUN is the fallback.
	PublicIPFromRouter bool `yaml:"publicIpFromRouter"`
	// STUN servers handed to devices and used for public IP detection.
	STUN []string `yaml:"stun"`
	// Interfaces optionally restricts ICE host candidates to these interface
	// names (e.g. ["eth0"]). Empty: all except loopback and container bridges.
	Interfaces []string `yaml:"interfaces"`
	// IncludeLoopback adds loopback candidates. Only useful for tests.
	IncludeLoopback bool `yaml:"includeLoopback"`
}

type APNs struct {
	KeyFile string `yaml:"keyFile"`
	KeyID   string `yaml:"keyId"`
	TeamID  string `yaml:"teamId"`
	// Topic is "<bundle id>.voip".
	Topic string `yaml:"topic"`
}

// Enabled reports whether VoIP pushes can be sent.
func (a APNs) Enabled() bool {
	return a.KeyFile != "" && a.KeyID != "" && a.TeamID != "" && a.Topic != ""
}

type Log struct {
	// Level: debug, info, warn, error.
	Level string `yaml:"level"`
	// ShowNumbers logs phone numbers and caller names in full. By default
	// numbers are masked to their last three digits and names are left out.
	ShowNumbers bool `yaml:"showNumbers"`
}

// Default returns a configuration with all defaults applied.
func Default() Config {
	return Config{
		Bridge: Bridge{
			Name: "Zuhause",
			// Only reachable locally (cloudflared runs on the same host).
			Listen: "127.0.0.1:8080",
			// Home network only (source filter), for pairing and at home.
			PrivateListen: ":8081",
			DataDir:       "data",
			MaxCalls:      8,
		},
		SIP: SIP{
			Registrar:             "fritz.box",
			Port:                  5060,
			BindPort:              5062,
			RTPPortMin:            40000,
			RTPPortMax:            40199,
			RegisterExpirySeconds: 300,
		},
		Media: Media{
			UDPPort:            50000,
			DetectPublicIP:     true,
			PublicIPFromRouter: true,
			STUN:               []string{"stun:stun.cloudflare.com:3478"},
		},
		APNs: APNs{
			TeamID: "T9CA6D7T8N",
			Topic:  "com.jorisconrad.housephone.voip",
		},
		FritzBox: FritzBox{
			Port:        49000,
			Timezone:    "Europe/Berlin",
			CountryCode: "49",
		},
		Log: Log{Level: "info"},
	}
}

// Load reads the YAML file at path (optional if it does not exist and
// allowMissing is true) and applies environment overrides.
func Load(path string, allowMissing bool) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist) && allowMissing:
	default:
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := cfg.applyEnv(os.LookupEnv); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyEnv(lookup func(string) (string, bool)) error {
	set := func(env string, dst *string) {
		if v, ok := lookup(env); ok && v != "" {
			*dst = v
		}
	}
	set(EnvDataDir, &c.Bridge.DataDir)
	set(EnvPublicURL, &c.Bridge.PublicURL)
	set(EnvLanURL, &c.Bridge.LanURL)
	set(EnvSIPPassword, &c.SIP.Password)
	set(EnvAPNsKeyFile, &c.APNs.KeyFile)
	set(EnvAPNsKeyID, &c.APNs.KeyID)
	set(EnvAPNsTeamID, &c.APNs.TeamID)
	set(EnvPublicIP, &c.Media.PublicIP)
	set(EnvLogLevel, &c.Log.Level)
	set(EnvFritzBoxPassword, &c.FritzBox.Password)
	for _, f := range []struct {
		env string
		dst *string
	}{{EnvSIPPasswordFile, &c.SIP.Password}, {EnvFritzBoxPasswordFile, &c.FritzBox.Password}} {
		if path, ok := lookup(f.env); ok && path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", f.env, err)
			}
			*f.dst = strings.TrimSpace(string(data))
		}
	}
	return nil
}

// FritzBoxHost is the TR-064 host: fritzbox.host or sip.registrar.
func (c Config) FritzBoxHost() string {
	if c.FritzBox.Host != "" {
		return c.FritzBox.Host
	}
	return c.SIP.Registrar
}

// ValidateServe checks everything the serve command needs.
func (c Config) ValidateServe() error {
	var errs []error
	if c.Bridge.Listen == "" {
		errs = append(errs, errors.New("bridge.listen is required"))
	}
	if c.Bridge.DataDir == "" {
		errs = append(errs, errors.New("bridge.dataDir is required"))
	}
	if c.SIP.Registrar == "" {
		errs = append(errs, errors.New("sip.registrar is required"))
	}
	if _, err := c.Bridge.TrustedProxyNets(); err != nil {
		errs = append(errs, err)
	}
	if _, err := c.Bridge.TrustedNetworkNets(); err != nil {
		errs = append(errs, err)
	}
	if c.Bridge.PrivateListen != "" {
		if _, _, err := net.SplitHostPort(c.Bridge.PrivateListen); err != nil {
			errs = append(errs, fmt.Errorf("bridge.privateListen: %w", err))
		}
	}
	if c.Bridge.LanURL != "" {
		if err := validateWSURL("bridge.lanUrl", c.Bridge.LanURL); err != nil {
			errs = append(errs, err)
		}
	}
	if c.Bridge.MaxCalls < 1 || c.Bridge.MaxCalls > 100 {
		errs = append(errs, fmt.Errorf("bridge.maxCalls must be between 1 and 100, got %d", c.Bridge.MaxCalls))
	}
	if c.SIP.Username == "" {
		errs = append(errs, errors.New("sip.username is required"))
	}
	if c.SIP.Password == "" {
		errs = append(errs, fmt.Errorf("sip.password is required (or %s / %s)", EnvSIPPassword, EnvSIPPasswordFile))
	}
	if err := validPort("sip.port", c.SIP.Port); err != nil {
		errs = append(errs, err)
	}
	if err := validPort("sip.bindPort", c.SIP.BindPort); err != nil {
		errs = append(errs, err)
	}
	if err := validPort("media.udpPort", c.Media.UDPPort); err != nil {
		errs = append(errs, err)
	}
	if c.SIP.RTPPortMin <= 0 || c.SIP.RTPPortMax < c.SIP.RTPPortMin || c.SIP.RTPPortMax > 65535 {
		errs = append(errs, fmt.Errorf("sip.rtpPortMin/rtpPortMax invalid: %d-%d", c.SIP.RTPPortMin, c.SIP.RTPPortMax))
	}
	if c.SIP.RegisterExpirySeconds < 60 {
		errs = append(errs, errors.New("sip.registerExpirySeconds must be at least 60"))
	}
	if c.Media.PublicIP != "" && !isIPv4(c.Media.PublicIP) {
		errs = append(errs, fmt.Errorf("media.publicIp %q is not an IPv4 address", c.Media.PublicIP))
	}
	if c.Bridge.PublicURL != "" {
		if err := ValidatePublicURL(c.Bridge.PublicURL); err != nil {
			errs = append(errs, err)
		}
	}
	if c.FritzBox.Enabled() {
		if c.FritzBox.Password == "" {
			errs = append(errs, fmt.Errorf("fritzbox.password is required when fritzbox.username is set (or %s / %s)", EnvFritzBoxPassword, EnvFritzBoxPasswordFile))
		}
		if err := validPort("fritzbox.port", c.FritzBox.Port); err != nil {
			errs = append(errs, err)
		}
		if _, err := time.LoadLocation(c.FritzBox.Timezone); err != nil || c.FritzBox.Timezone == "" {
			errs = append(errs, fmt.Errorf("fritzbox.timezone %q is not a known time zone", c.FritzBox.Timezone))
		}
		if !isDigits(c.FritzBox.CountryCode) {
			errs = append(errs, fmt.Errorf("fritzbox.countryCode must be digits without \"+\", got %q", c.FritzBox.CountryCode))
		}
	}
	apnsPartial := c.APNs.KeyFile != "" || c.APNs.KeyID != ""
	if apnsPartial && !c.APNs.Enabled() {
		errs = append(errs, errors.New("apns: keyFile, keyId, teamId and topic must all be set"))
	}
	return errors.Join(errs...)
}

// ValidatePair checks everything the pair command needs.
func (c Config) ValidatePair() error {
	if c.Bridge.DataDir == "" {
		return errors.New("bridge.dataDir is required")
	}
	if c.Bridge.PublicURL == "" {
		return fmt.Errorf("bridge.publicUrl is required for pairing (or %s)", EnvPublicURL)
	}
	if err := ValidatePublicURL(c.Bridge.PublicURL); err != nil {
		return err
	}
	// Devices pair only over the private listener (home network).
	if c.Bridge.PrivateListen == "" && c.Bridge.LanURL == "" {
		return errors.New("bridge.privateListen is required for pairing: devices pair in the home network")
	}
	if c.Bridge.LanURL != "" {
		return validateWSURL("bridge.lanUrl", c.Bridge.LanURL)
	}
	return nil
}

// ValidatePublicURL requires a ws:// or wss:// URL with a host.
func ValidatePublicURL(raw string) error {
	return validateWSURL("bridge.publicUrl", raw)
}

func validateWSURL(field, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if u.Scheme != "wss" && u.Scheme != "ws" {
		return fmt.Errorf("%s must start with wss:// or ws:// (got %q)", field, raw)
	}
	if u.Host == "" {
		return fmt.Errorf("%s has no host: %q", field, raw)
	}
	return nil
}

func validPort(name string, p int) error {
	if p <= 0 || p > 65535 {
		return fmt.Errorf("%s must be a port number, got %d", name, p)
	}
	return nil
}

func isIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 || strconv.Itoa(n) != p {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	if s == "" || len(s) > 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
