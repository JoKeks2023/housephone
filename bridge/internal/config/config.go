// Package config loads the bridge configuration from YAML with environment
// overrides for secrets.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Environment variables that override configuration values.
const (
	EnvConfig          = "HOUSEPHONE_CONFIG"
	EnvDataDir         = "HOUSEPHONE_DATA_DIR"
	EnvPublicURL       = "HOUSEPHONE_PUBLIC_URL"
	EnvSIPPassword     = "HOUSEPHONE_SIP_PASSWORD"
	EnvSIPPasswordFile = "HOUSEPHONE_SIP_PASSWORD_FILE"
	EnvAPNsKeyFile     = "HOUSEPHONE_APNS_KEY_FILE"
	EnvAPNsKeyID       = "HOUSEPHONE_APNS_KEY_ID"
	EnvAPNsTeamID      = "HOUSEPHONE_APNS_TEAM_ID"
	EnvPublicIP        = "HOUSEPHONE_PUBLIC_IP"
	EnvLogLevel        = "HOUSEPHONE_LOG_LEVEL"
)

// DefaultConfigPath is used when neither a flag nor HOUSEPHONE_CONFIG is set.
const DefaultConfigPath = "config.yaml"

type Config struct {
	Bridge Bridge `yaml:"bridge"`
	SIP    SIP    `yaml:"sip"`
	Media  Media  `yaml:"media"`
	APNs   APNs   `yaml:"apns"`
	Log    Log    `yaml:"log"`
}

type Bridge struct {
	// Name is shown in the app, e.g. "Zuhause".
	Name string `yaml:"name"`
	// Listen is the HTTP/WebSocket listen address, e.g. ":8080".
	Listen string `yaml:"listen"`
	// PublicURL is the wss:// URL devices use, embedded in pairing links.
	PublicURL string `yaml:"publicUrl"`
	// DataDir holds devices.json, pairing.json and bridge.json.
	DataDir string `yaml:"dataDir"`
	// TrustProxyHeaders uses CF-Connecting-IP / X-Forwarded-For as client IP.
	// Enable only behind Cloudflare Tunnel or a reverse proxy.
	TrustProxyHeaders bool `yaml:"trustProxyHeaders"`
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
}

// Default returns a configuration with all defaults applied.
func Default() Config {
	return Config{
		Bridge: Bridge{
			Name:    "Zuhause",
			Listen:  ":8080",
			DataDir: "data",
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
	set(EnvSIPPassword, &c.SIP.Password)
	set(EnvAPNsKeyFile, &c.APNs.KeyFile)
	set(EnvAPNsKeyID, &c.APNs.KeyID)
	set(EnvAPNsTeamID, &c.APNs.TeamID)
	set(EnvPublicIP, &c.Media.PublicIP)
	set(EnvLogLevel, &c.Log.Level)
	if path, ok := lookup(EnvSIPPasswordFile); ok && path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", EnvSIPPasswordFile, err)
		}
		c.SIP.Password = strings.TrimSpace(string(data))
	}
	return nil
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
	return ValidatePublicURL(c.Bridge.PublicURL)
}

// ValidatePublicURL requires a ws:// or wss:// URL with a host.
func ValidatePublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bridge.publicUrl: %w", err)
	}
	if u.Scheme != "wss" && u.Scheme != "ws" {
		return fmt.Errorf("bridge.publicUrl must start with wss:// (got %q)", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("bridge.publicUrl has no host: %q", raw)
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
