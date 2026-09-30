package dashboard

import (
	"net"
	"net/http"
	"net/netip"
	"regexp"
)

// Gate is the access layer in front of the dashboard. The pages don't
// know who is allowed in or where they are mounted; the gate decides both.
//
//   - SupervisorGate (Home Assistant add-on): only the Supervisor's ingress
//     proxy may connect; Home Assistant has authenticated the user and
//     panel_admin limits the panel to administrators.
//   - HPHN-39 will add a passkey gate for plain Docker, served on the
//     private listener (home network / Tailscale). It plugs in here.
type Gate interface {
	// Guard wraps the dashboard and rejects requests that are not allowed.
	Guard(next http.Handler) http.Handler
	// BasePath is the URL prefix the browser sees for r, without a
	// trailing slash ("" when mounted at the root).
	BasePath(r *http.Request) string
}

// SupervisorGate admits only the Home Assistant ingress proxy.
type SupervisorGate struct {
	// Proxy is the ingress proxy's address (172.30.32.2).
	Proxy netip.Addr
}

// ingressPath is what the Supervisor sends in X-Ingress-Path.
var ingressPath = regexp.MustCompile(`^/api/hassio_ingress/[A-Za-z0-9_-]+$`)

func (g SupervisorGate) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.fromProxy(r.RemoteAddr) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// fromProxy looks at the TCP peer only; headers can be forged.
func (g SupervisorGate) fromProxy(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap() == g.Proxy
}

func (g SupervisorGate) BasePath(r *http.Request) string {
	if p := r.Header.Get("X-Ingress-Path"); ingressPath.MatchString(p) {
		return p
	}
	return ""
}
