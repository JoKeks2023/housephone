package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pion/stun/v3"
)

// PublicIPSource keeps the public IPv4 address current. It is advertised to
// devices as a server-reflexive ICE candidate on the forwarded media port.
type PublicIPSource struct {
	mu     sync.RWMutex
	ip     string
	source string
	log    *slog.Logger
}

// PublicIPConfig selects how the public IP is found. The first non-empty
// option wins.
type PublicIPConfig struct {
	// Static public IPv4 address.
	Static string
	// Host is resolved every 5 minutes (e.g. a MyFRITZ!/DynDNS name).
	Host string
	// Router is the FRITZ!Box host. When set and Detect is true, the router
	// is asked for its external IPv4 via UPnP every 30 seconds. That notices
	// a changing IP within seconds and needs no internet round trip.
	Router string
	// STUN servers (e.g. "stun:stun.cloudflare.com:3478"), queried at most
	// every 10 minutes when Detect is true and the router does not answer.
	STUN   []string
	Detect bool
}

const (
	routerInterval = 30 * time.Second
	stunInterval   = 10 * time.Minute
	upnpPort       = "49000"
)

// NewPublicIPSource starts resolving in the background until ctx ends.
func NewPublicIPSource(ctx context.Context, cfg PublicIPConfig, log *slog.Logger) *PublicIPSource {
	p := &PublicIPSource{log: log.With("component", "public-ip")}
	switch {
	case cfg.Static != "":
		p.set(cfg.Static, "config")
	case cfg.Host != "":
		go p.loop(ctx, 5*time.Minute, func(ctx context.Context) (string, string, error) {
			ip, err := resolveIPv4(ctx, cfg.Host)
			return ip, "dns " + cfg.Host, err
		})
	case cfg.Detect && (cfg.Router != "" || len(cfg.STUN) > 0):
		d := &detector{current: p.Get, now: time.Now, stunEvery: stunInterval, log: p.log}
		every := stunInterval
		if cfg.Router != "" {
			baseURL := "http://" + net.JoinHostPort(cfg.Router, upnpPort)
			client := &http.Client{Timeout: 5 * time.Second}
			d.router = func(ctx context.Context) (string, error) { return routerPublicIP(ctx, client, baseURL) }
			every = routerInterval
		}
		if len(cfg.STUN) > 0 {
			d.stun = func(ctx context.Context) (string, error) { return stunPublicIPAny(ctx, cfg.STUN) }
		}
		go p.loop(ctx, every, d.lookup)
	default:
		p.log.Warn("no public IP configured: devices outside the home network cannot reach the media port")
	}
	return p
}

// Get returns the current public IP or "".
func (p *PublicIPSource) Get() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ip
}

// Source names where the current IP came from (config, fritzbox, stun, dns …).
func (p *PublicIPSource) Source() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.source
}

func (p *PublicIPSource) set(ip, source string) {
	p.mu.Lock()
	changed := p.ip != ip
	p.ip = ip
	p.source = source
	p.mu.Unlock()
	if changed {
		p.log.Info("public IP", "ip", ip, "source", source)
	}
}

func (p *PublicIPSource) loop(ctx context.Context, every time.Duration, lookup func(context.Context) (ip, source string, err error)) {
	for {
		lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ip, source, err := lookup(lookupCtx)
		cancel()
		if err != nil {
			p.log.Warn("public IP lookup failed", "error", err)
		} else {
			p.set(ip, source)
		}
		wait := every
		if err != nil && p.Get() == "" {
			wait = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// detector asks the router first and falls back to STUN, but queries STUN
// at most every stunEvery so a router without UPnP doesn't cause a STUN
// request every 30 seconds.
type detector struct {
	router    func(context.Context) (string, error)
	stun      func(context.Context) (string, error)
	stunEvery time.Duration
	lastSTUN  time.Time
	current   func() string
	now       func() time.Time
	log       *slog.Logger

	routerFailing bool
}

func (d *detector) lookup(ctx context.Context) (string, string, error) {
	var routerErr error
	if d.router != nil {
		ip, err := d.router(ctx)
		if err == nil {
			if d.routerFailing {
				d.log.Info("FRITZ!Box reports its external IP again")
				d.routerFailing = false
			}
			return ip, "fritzbox", nil
		}
		routerErr = err
		if !d.routerFailing {
			d.log.Info("FRITZ!Box does not report its external IP via UPnP; using STUN", "error", err)
			d.routerFailing = true
		}
	}
	if d.stun == nil {
		return "", "", routerErr
	}
	if current := d.current(); current != "" && !d.lastSTUN.IsZero() && d.now().Sub(d.lastSTUN) < d.stunEvery {
		return current, "stun", nil
	}
	ip, err := d.stun(ctx)
	d.lastSTUN = d.now()
	if err != nil {
		return "", "", errors.Join(routerErr, err)
	}
	return ip, "stun", nil
}

// routerPublicIP asks a FRITZ!Box (or any UPnP IGD) for its external IPv4.
// FRITZ!OS answers without authentication when "Statusinformationen über
// UPnP übertragen" is enabled (the default).
func routerPublicIP(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	services := []struct{ path, urn string }{
		{"/igdupnp/control/WANIPConn1", "urn:schemas-upnp-org:service:WANIPConnection:1"},
		{"/igdupnp/control/WANPPPConn1", "urn:schemas-upnp-org:service:WANPPPConnection:1"},
	}
	var errs []error
	for _, svc := range services {
		ip, err := upnpExternalIP(ctx, client, baseURL+svc.path, svc.urn)
		if err == nil {
			return ip, nil
		}
		errs = append(errs, err)
	}
	return "", errors.Join(errs...)
}

var externalIPPattern = regexp.MustCompile(`<NewExternalIPAddress>\s*([^<\s]*)\s*</NewExternalIPAddress>`)

func upnpExternalIP(ctx context.Context, client *http.Client, url, urn string) (string, error) {
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>` +
		`<u:GetExternalIPAddress xmlns:u="` + urn + `"/></s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+urn+`#GetExternalIPAddress"`)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upnp %s: HTTP %d", url, resp.StatusCode)
	}
	match := externalIPPattern.FindSubmatch(data)
	if match == nil {
		return "", fmt.Errorf("upnp %s: no external IP in response", url)
	}
	ip := string(match[1])
	if !isPublicIPv4(ip) {
		return "", fmt.Errorf("upnp %s: %q is not a public IPv4 address", url, ip)
	}
	return ip, nil
}

var carrierGradeNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isPublicIPv4 rejects addresses a router reports behind DS-Lite or CGNAT
// (empty, private, shared 100.64/10) that devices on the internet can't reach.
func isPublicIPv4(s string) bool {
	ip := net.ParseIP(s).To4()
	if ip == nil {
		return false
	}
	return !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsMulticast() && !carrierGradeNAT.Contains(ip) && !ip.Equal(net.IPv4bcast)
}

func stunPublicIPAny(ctx context.Context, servers []string) (string, error) {
	var errs []error
	for _, server := range servers {
		serverCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		ip, err := stunPublicIP(serverCtx, server)
		cancel()
		if err == nil {
			return ip, nil
		}
		errs = append(errs, err)
	}
	return "", errors.Join(errs...)
}

func resolveIPv4(ctx context.Context, host string) (string, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("%s has no IPv4 address", host)
	}
	return ips[0].String(), nil
}

// stunPublicIP asks a STUN server for our mapped IPv4 address.
func stunPublicIP(ctx context.Context, server string) (string, error) {
	addr := strings.TrimPrefix(server, "stun:")
	addr, _, _ = strings.Cut(addr, "?")
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", addr)
	if err != nil {
		return "", err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := stun.NewClient(conn)
	if err != nil {
		conn.Close()
		return "", err
	}
	defer client.Close()

	type result struct {
		ip  string
		err error
	}
	done := make(chan result, 1)
	go func() {
		var res result
		err := client.Do(stun.MustBuild(stun.TransactionID, stun.BindingRequest), func(ev stun.Event) {
			if ev.Error != nil {
				res.err = ev.Error
				return
			}
			var mapped stun.XORMappedAddress
			if err := mapped.GetFrom(ev.Message); err != nil {
				res.err = err
				return
			}
			res.ip = mapped.IP.String()
		})
		if err != nil {
			res.err = err
		}
		done <- res
	}()
	var ip string
	select {
	case res := <-done:
		if res.err != nil {
			return "", fmt.Errorf("stun %s: %w", addr, res.err)
		}
		ip = res.ip
	case <-ctx.Done():
		return "", fmt.Errorf("stun %s: %w", addr, ctx.Err())
	}
	if net.ParseIP(ip).To4() == nil {
		return "", fmt.Errorf("stun %s returned non-IPv4 %q", addr, ip)
	}
	return ip, nil
}
