// Package lan makes sure the bridge only talks to its FRITZ!Box inside the
// home network.
//
// The FRITZ!Box is usually configured by name ("fritz.box"). That name only
// resolves to the router when the server uses the FRITZ!Box as its DNS
// server. With another resolver (Pi-hole, 1.1.1.1, …) "fritz.box" is a public
// domain owned by a third party, and the bridge would send its SIP digest
// login and TR-064 credentials there. Resolve rejects such targets, and the
// bridge pins the resolved address at startup so later DNS changes cannot
// redirect credentials either.
package lan

import (
	"context"
	"fmt"
	"net"
)

// IsLocal reports whether ip is an address inside a home network: private
// (RFC 1918), unique local (fc00::/7), link-local or loopback.
func IsLocal(ip net.IP) bool {
	return ip != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLoopback())
}

// Resolve looks up host (a name or an IP literal). It fails unless every
// address the name resolves to is local; it then returns the first one,
// preferring IPv4.
func Resolve(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if !IsLocal(ip) {
			return nil, notLocal(host, ip)
		}
		return ip, nil
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("%s lässt sich nicht auflösen (%w) – trag die IP deiner FRITZ!Box ein, z. B. 192.168.178.1", host, err)
	}
	var first net.IP
	for _, ip := range ips {
		if !IsLocal(ip) {
			return nil, notLocal(host, ip)
		}
		if first == nil || (first.To4() == nil && ip.To4() != nil) {
			first = ip
		}
	}
	if first == nil {
		return nil, fmt.Errorf("%s hat keine Adresse – trag die IP deiner FRITZ!Box ein, z. B. 192.168.178.1", host)
	}
	return first, nil
}

func notLocal(host string, ip net.IP) error {
	return fmt.Errorf("%s löst auf %s auf – das ist keine Adresse in deinem Heimnetz. "+
		"Die Bridge schickt dorthin keine Zugangsdaten. Trag die IP deiner FRITZ!Box ein (z. B. 192.168.178.1 oder 192.168.0.1)", host, ip)
}
