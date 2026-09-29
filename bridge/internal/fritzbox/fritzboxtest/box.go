// Package fritzboxtest emulates the TR-064 parts of a FRITZ!Box for tests:
// GetSecurityPort over plain HTTP, X_AVM-DE_OnTel over TLS with HTTP digest
// auth, and phonebook/call list downloads authorized by a session ID. The
// served XML follows AVM's x_contactSCPD v41 examples.
package fritzboxtest

import (
	"embed"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/icholy/digest"
)

//go:embed testdata/*.xml
var testdata embed.FS

// Credentials of the fake FRITZ!Box user.
const (
	Username = "housephone"
	Password = "geheim"
)

const (
	nonce          = "8e3a1f0c9b7d2e4a"
	sid            = "a1b2c3d4e5f60718"
	deviceInfo     = "/upnp/control/deviceinfo"
	deviceInfoURN  = "urn:dslforum-org:service:DeviceInfo:1"
	onTel          = "/upnp/control/x_contact"
	onTelURN       = "urn:dslforum-org:service:X_AVM-DE_OnTel:1"
	fritzBoxOrigin = "https://fritz.box:49443"
)

// Box is a running fake.
type Box struct {
	// PhonebookFetches and CallListFetches count downloads.
	PhonebookFetches atomic.Int32
	CallListFetches  atomic.Int32

	plain *httptest.Server

	mu             sync.Mutex
	secure         *httptest.Server
	phonebooks     map[string][]byte
	phonebookNames map[string]string
	callList       []byte
	callListOff    bool
	faultCode      string
	lastMax        string
	actions        map[string]int
}

// Start runs the fake until the test ends. Phonebook 0 ("Telefonbuch") and
// the call list produce docs/protocol/fixtures/http/*.json; phonebook 1
// ("Geschäftlich", ISO-8859-1) only holds a fax number.
func Start(t testing.TB) *Box {
	t.Helper()
	b := &Box{
		phonebooks:     map[string][]byte{"0": mustRead(t, "phonebook0.xml"), "1": mustRead(t, "phonebook1.xml")},
		phonebookNames: map[string]string{"0": "Telefonbuch", "1": "Geschäftlich"},
		callList:       mustRead(t, "calllist.xml"),
		actions:        map[string]int{},
	}
	b.secure = httptest.NewTLSServer(http.HandlerFunc(b.serveSecure))
	b.plain = httptest.NewServer(http.HandlerFunc(b.servePlain))
	t.Cleanup(func() {
		b.plain.Close()
		b.mu.Lock()
		b.secure.Close()
		b.mu.Unlock()
	})
	return b
}

func mustRead(t testing.TB, name string) []byte {
	t.Helper()
	data, err := testdata.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Host is the address to configure as fritzbox.host.
func (b *Box) Host() string { return "127.0.0.1" }

// PlainPort is the TR-064 port (49000 on a real FRITZ!Box).
func (b *Box) PlainPort() string { return port(b.plain) }

// SetFault makes every X_AVM-DE_OnTel action answer with a UPnP error.
func (b *Box) SetFault(code string) {
	b.mu.Lock()
	b.faultCode = code
	b.mu.Unlock()
}

// SetCallListOff simulates a switched-off call list (empty URL).
func (b *Box) SetCallListOff(off bool) {
	b.mu.Lock()
	b.callListOff = off
	b.mu.Unlock()
}

// LastMax returns the max parameter of the last call list download.
func (b *Box) LastMax() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastMax
}

// ActionCount counts authenticated calls of an action.
func (b *Box) ActionCount(action string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.actions[action]
}

// RestartTLS moves the TLS endpoint to a new port, like a FRITZ!Box reboot.
func (b *Box) RestartTLS() {
	b.mu.Lock()
	old := b.secure
	old.Close()
	b.secure = httptest.NewTLSServer(old.Config.Handler)
	b.mu.Unlock()
}

func port(srv *httptest.Server) string {
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return p
}

func (b *Box) servePlain(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != deviceInfo || !strings.HasSuffix(soapAction(r), "#GetSecurityPort") {
		http.NotFound(w, r)
		return
	}
	b.mu.Lock()
	securePort := port(b.secure)
	b.mu.Unlock()
	writeSOAP(w, "GetSecurityPort", deviceInfoURN, "<NewSecurityPort>"+securePort+"</NewSecurityPort>")
}

func (b *Box) serveSecure(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/phonebook.lua":
		b.serveDownload(w, r, func() ([]byte, bool) {
			b.PhonebookFetches.Add(1)
			b.mu.Lock()
			defer b.mu.Unlock()
			data, ok := b.phonebooks[r.URL.Query().Get("pbid")]
			return data, ok
		})
	case "/calllist.lua":
		b.serveDownload(w, r, func() ([]byte, bool) {
			b.CallListFetches.Add(1)
			b.mu.Lock()
			defer b.mu.Unlock()
			b.lastMax = r.URL.Query().Get("max")
			return b.callList, true
		})
	case onTel:
		if !checkDigest(r) {
			w.Header().Set("WWW-Authenticate", challenge().String())
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b.serveOnTel(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (b *Box) serveDownload(w http.ResponseWriter, r *http.Request, content func() ([]byte, bool)) {
	if r.URL.Query().Get("sid") != sid {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	data, ok := content()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	_, _ = w.Write(data)
}

// challenge is the digest challenge of a FRITZ!Box (realm "HTTPS Access").
func challenge() *digest.Challenge {
	return &digest.Challenge{Realm: "HTTPS Access", Nonce: nonce, Algorithm: "MD5", QOP: []string{"auth"}}
}

func checkDigest(r *http.Request) bool {
	creds, err := digest.ParseCredentials(r.Header.Get("Authorization"))
	if err != nil || creds.Username != Username || creds.Nonce != nonce {
		return false
	}
	want, err := digest.Digest(challenge(), digest.Options{
		Method:   r.Method,
		URI:      creds.URI,
		Username: Username,
		Password: Password,
		Cnonce:   creds.Cnonce,
		Count:    creds.Nc,
	})
	return err == nil && want.Response == creds.Response
}

var phonebookIDPattern = regexp.MustCompile(`<NewPhonebookID>(\d+)</NewPhonebookID>`)

func (b *Box) serveOnTel(w http.ResponseWriter, r *http.Request) {
	action := soapAction(r)
	action = action[strings.LastIndex(action, "#")+1:]
	body, _ := io.ReadAll(r.Body)
	b.mu.Lock()
	b.actions[action]++
	fault, off := b.faultCode, b.callListOff
	b.mu.Unlock()
	if fault != "" {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault>`+
			`<faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail>`+
			`<UPnPError xmlns="urn:dslforum-org:control-1-0"><errorCode>%s</errorCode><errorDescription>Action Not Authorized</errorDescription></UPnPError>`+
			`</detail></s:Fault></s:Body></s:Envelope>`, fault)
		return
	}
	// URLs as the FRITZ!Box returns them: its own name and TLS port. The
	// client must rewrite them to the configured host.
	switch action {
	case "GetPhonebookList":
		writeSOAP(w, action, onTelURN, "<NewPhonebookList>0,1</NewPhonebookList>")
	case "GetPhonebook":
		m := phonebookIDPattern.FindSubmatch(body)
		if m == nil {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		id := string(m[1])
		b.mu.Lock()
		name := b.phonebookNames[id]
		b.mu.Unlock()
		writeSOAP(w, action, onTelURN, fmt.Sprintf(
			"<NewPhonebookName>%s</NewPhonebookName><NewPhonebookExtraID></NewPhonebookExtraID>"+
				"<NewPhonebookURL>%s/phonebook.lua?sid=%s&amp;pbid=%s</NewPhonebookURL>",
			name, fritzBoxOrigin, sid, id))
	case "GetCallList":
		url := ""
		if !off {
			url = fritzBoxOrigin + "/calllist.lua?sid=" + sid
		}
		writeSOAP(w, action, onTelURN, "<NewCallListURL>"+url+"</NewCallListURL>")
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func soapAction(r *http.Request) string {
	return strings.Trim(r.Header.Get("SOAPAction"), `"`)
}

func writeSOAP(w http.ResponseWriter, action, service, inner string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	fmt.Fprintf(w, `<?xml version="1.0"?>`+"\n"+
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`+
		`<s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`, action, service, inner, action)
}
