package fritzbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The session ID in download URLs must not show up in errors (they are
// logged).
func TestDownloadErrorsDoNotContainSessionID(t *testing.T) {
	const secretSID = "5e55101d5ec2e7"
	secure := httptest.NewUnstartedServer(nil)
	secure.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.Header.Get("SOAPAction"), "#GetPhonebookList"):
			writeTestSOAP(w, "GetPhonebookList", onTelService, "<NewPhonebookList>0</NewPhonebookList>")
		case strings.Contains(r.Header.Get("SOAPAction"), "#GetPhonebook"):
			url := fmt.Sprintf("https://%s/phonebook.lua?sid=%s&pbid=0", secure.Listener.Addr(), secretSID)
			writeTestSOAP(w, "GetPhonebook", onTelService, "<NewPhonebookURL>"+url+"</NewPhonebookURL><NewPhonebookName>Telefonbuch</NewPhonebookName><NewPhonebookExtraID></NewPhonebookExtraID>")
		default:
			// The download: drop the connection.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
		}
	})
	secure.StartTLS()
	defer secure.Close()
	_, securePort, _ := net.SplitHostPort(secure.Listener.Addr().String())

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestSOAP(w, "GetSecurityPort", deviceInfoService, "<NewSecurityPort>"+securePort+"</NewSecurityPort>")
	}))
	defer plain.Close()
	host, plainPort, _ := net.SplitHostPort(plain.Listener.Addr().String())

	c := NewClient(ClientConfig{Host: host, PlainPort: plainPort, Username: "housephone", Password: "geheim"})
	_, err := c.Phonebook(context.Background())
	if err == nil {
		t.Fatal("expected a download error")
	}
	if strings.Contains(err.Error(), secretSID) {
		t.Fatalf("error leaks the session ID: %v", err)
	}
}

func writeTestSOAP(w http.ResponseWriter, action, service, inner string) {
	w.Header().Set("Content-Type", "text/xml")
	fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>`+
		`<u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`, action, service, inner, action)
}
