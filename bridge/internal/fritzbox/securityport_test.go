package fritzbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Security review N2: the security port arrives unauthenticated over plain
// HTTP. A forged value must not change the host the credentials go to.
func TestForgedSecurityPortIsRejected(t *testing.T) {
	var evilHits atomic.Int32
	evil := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { evilHits.Add(1) }))
	defer evil.Close()
	evilHost := evil.Listener.Addr().String()

	for _, forged := range []string{"1@" + evilHost, "49443/../x", "0", "70000", "-1", "", "443#frag"} {
		plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/xml")
			fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>`+
				`<u:GetSecurityPortResponse xmlns:u="%s"><NewSecurityPort>%s</NewSecurityPort></u:GetSecurityPortResponse></s:Body></s:Envelope>`,
				deviceInfoService, forged)
		}))
		host, plainPort, _ := net.SplitHostPort(plain.Listener.Addr().String())
		c := NewClient(ClientConfig{Host: host, PlainPort: plainPort, Username: "housephone", Password: "geheim"})
		_, err := c.Phonebook(context.Background())
		plain.Close()
		var fe *Error
		if err == nil || !errors.As(err, &fe) || fe.Kind != KindProtocol {
			t.Errorf("port %q: got %v, want a protocol error", forged, err)
		}
	}
	if n := evilHits.Load(); n != 0 {
		t.Fatalf("credentials were sent to another host %d time(s)", n)
	}
}
