// Package bonjour announces the bridge's private listener in the home
// network as _housephone._tcp (DNS-SD over mDNS), so the app finds it
// without a QR code (ADR-0007).
//
// The announcement carries nothing secret: the bridge name, the protocol
// and a short prefix of the bridge fingerprint, which lets the app tell an
// already paired bridge from a new one. Whoever answers in its name still
// has to pass the confirmation code (SAS) an admin compares.
package bonjour

import (
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/hashicorp/mdns"
)

// Service is the DNS-SD service type.
const Service = "_housephone._tcp"

// FingerprintPrefixLength is how much of the bridge fingerprint (base64url)
// goes into the TXT record: 48 bits, enough to recognize a bridge, far too
// little to matter for its security.
const FingerprintPrefixLength = 8

// Config describes the announcement.
type Config struct {
	// Name is the instance name, the bridge name ("Zuhause").
	Name string
	// IP is the bridge's address in the home network.
	IP net.IP
	// Port is the private listener's port.
	Port int
	// Fingerprint is the bridge fingerprint (base64url SHA-256).
	Fingerprint string
	Logger      *slog.Logger
}

// Announcer answers mDNS queries until Close.
type Announcer struct {
	server *mdns.Server
}

// TXT returns the TXT record.
func TXT(fingerprint string) []string {
	fp := fingerprint
	if len(fp) > FingerprintPrefixLength {
		fp = fp[:FingerprintPrefixLength]
	}
	return []string{"txtvers=1", "proto=hp2", "pair=lan", "fp=" + fp}
}

// InstanceName makes a DNS-SD instance name from the bridge name: at most
// 63 bytes, without control characters and dots.
func InstanceName(name string) string {
	clean := strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '.' || r == '\\' {
			return -1
		}
		return r
	}, name))
	for len(clean) > 63 {
		_, size := utf8.DecodeLastRuneInString(clean)
		clean = clean[:len(clean)-size]
	}
	if clean == "" {
		return "Housephone"
	}
	return clean
}

// Start announces the service.
func Start(cfg Config) (*Announcer, error) {
	if cfg.IP == nil || cfg.IP.IsUnspecified() || cfg.IP.IsLoopback() {
		return nil, errors.New("bonjour: no home network address")
	}
	if cfg.Port <= 0 {
		return nil, errors.New("bonjour: no port")
	}
	host := "housephone-" + strings.ReplaceAll(strings.ReplaceAll(cfg.IP.String(), ".", "-"), ":", "-") + ".local."
	zone, err := mdns.NewMDNSService(InstanceName(cfg.Name), Service, "local.", host, cfg.Port, []net.IP{cfg.IP}, TXT(cfg.Fingerprint))
	if err != nil {
		return nil, fmt.Errorf("bonjour: %w", err)
	}
	var iface *net.Interface
	if i, err := interfaceFor(cfg.IP); err == nil {
		iface = i
	}
	logger := log.New(io.Discard, "", 0)
	if cfg.Logger != nil {
		logger = slog.NewLogLogger(cfg.Logger.Handler(), slog.LevelDebug)
	}
	server, err := mdns.NewServer(&mdns.Config{Zone: zone, Iface: iface, Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("bonjour: %w", err)
	}
	return &Announcer{server: server}, nil
}

// Close stops answering.
func (a *Announcer) Close() error {
	if a == nil || a.server == nil {
		return nil
	}
	return a.server.Shutdown()
}

// interfaceFor finds the interface that holds ip.
func interfaceFor(ip net.IP) (*net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
				return &ifaces[i], nil
			}
		}
	}
	return nil, errors.New("no interface")
}

// AdvertisedIP picks the address to announce for the private listener
// bound to listenHost: the listener's own address if it is bound to one,
// otherwise lanIP. It returns nil if the listener is not reachable from
// the home network (bound to loopback or a non-private address such as a
// Tailscale IP).
func AdvertisedIP(listenHost, lanIP string) net.IP {
	host := listenHost
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = lanIP
	}
	ip := net.ParseIP(host)
	if ip == nil || !(ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return nil
	}
	return ip
}
