package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pion/stun/v3"
)

// PublicIPSource keeps the public IPv4 address current. It is advertised to
// devices as a server-reflexive ICE candidate on the forwarded media port.
type PublicIPSource struct {
	mu  sync.RWMutex
	ip  string
	log *slog.Logger
}

// PublicIPConfig selects how the public IP is found. The first non-empty
// option wins.
type PublicIPConfig struct {
	// Static public IPv4 address.
	Static string
	// Host is resolved every 5 minutes (e.g. a MyFRITZ!/DynDNS name).
	Host string
	// STUN servers (e.g. "stun:stun.cloudflare.com:3478") queried every 10
	// minutes when Detect is true.
	STUN   []string
	Detect bool
}

// NewPublicIPSource starts resolving in the background until ctx ends.
func NewPublicIPSource(ctx context.Context, cfg PublicIPConfig, log *slog.Logger) *PublicIPSource {
	p := &PublicIPSource{log: log.With("component", "public-ip")}
	switch {
	case cfg.Static != "":
		p.set(cfg.Static, "config")
	case cfg.Host != "":
		go p.loop(ctx, 5*time.Minute, "dns "+cfg.Host, func(ctx context.Context) (string, error) {
			return resolveIPv4(ctx, cfg.Host)
		})
	case cfg.Detect && len(cfg.STUN) > 0:
		go p.loop(ctx, 10*time.Minute, "stun", func(ctx context.Context) (string, error) {
			var errs []error
			for _, server := range cfg.STUN {
				serverCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				ip, err := stunPublicIP(serverCtx, server)
				cancel()
				if err == nil {
					return ip, nil
				}
				errs = append(errs, err)
			}
			return "", errors.Join(errs...)
		})
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

func (p *PublicIPSource) set(ip, source string) {
	p.mu.Lock()
	changed := p.ip != ip
	p.ip = ip
	p.mu.Unlock()
	if changed {
		p.log.Info("public IP", "ip", ip, "source", source)
	}
}

func (p *PublicIPSource) loop(ctx context.Context, every time.Duration, source string, lookup func(context.Context) (string, error)) {
	for {
		lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ip, err := lookup(lookupCtx)
		cancel()
		if err != nil {
			p.log.Warn("public IP lookup failed", "source", source, "error", err)
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
