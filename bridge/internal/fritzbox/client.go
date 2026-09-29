// Package fritzbox reads the FRITZ!Box phonebook and call list via TR-064
// (service X_AVM-DE_OnTel, AVM x_contactSCPD) for GET /v1/phonebook and
// GET /v1/history (signaling v1.2, ADR-0003). It never changes anything on
// the FRITZ!Box.
package fritzbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	// The Docker image has no zoneinfo; the call list is in local time.
	_ "time/tzdata"

	"github.com/icholy/digest"
)

// TR-064 constants (AVM "TR-064 First Steps", x_contactSCPD v41).
const (
	plainPort          = "49000"
	deviceInfoControl  = "/upnp/control/deviceinfo"
	deviceInfoService  = "urn:dslforum-org:service:DeviceInfo:1"
	onTelControl       = "/upnp/control/x_contact"
	onTelService       = "urn:dslforum-org:service:X_AVM-DE_OnTel:1"
	maxDownloadBytes   = 8 << 20
	defaultHTTPTimeout = 15 * time.Second
)

// ErrorKind classifies why the FRITZ!Box could not deliver data.
type ErrorKind int

const (
	// KindUnreachable: no connection (wrong host, TR-064 disabled).
	KindUnreachable ErrorKind = iota
	// KindAuth: login rejected or the user lacks the "Phone" right.
	KindAuth
	// KindUnsupported: the feature is disabled on the FRITZ!Box.
	KindUnsupported
	// KindProtocol: unexpected answer.
	KindProtocol
	// KindNotConfigured: fritzbox.username is not set.
	KindNotConfigured
)

// Error is a FRITZ!Box failure with a message for the user of the app.
type Error struct {
	Kind ErrorKind
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("fritzbox: %v", e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// UserMessage explains the failure in German for the app (error.message).
func (e *Error) UserMessage() string {
	switch e.Kind {
	case KindNotConfigured:
		return "Telefonbuch und Anrufliste sind in der Bridge nicht eingerichtet (fritzbox.username in config.yaml)."
	case KindAuth:
		return "Die FRITZ!Box hat die Anmeldung abgelehnt. Prüfe Benutzer, Kennwort und das Recht „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“."
	case KindUnsupported:
		return "Die FRITZ!Box stellt diese Liste nicht bereit (Funktion in der FRITZ!Box ausgeschaltet?)."
	case KindUnreachable:
		return "Die FRITZ!Box ist nicht erreichbar. Ist „Zugriff für Anwendungen zulassen“ (Heimnetz → Netzwerk → Netzwerkeinstellungen) eingeschaltet?"
	default:
		return "Die FRITZ!Box hat unerwartet geantwortet."
	}
}

// ErrNotConfigured is returned when TR-064 is not set up.
var ErrNotConfigured = &Error{Kind: KindNotConfigured, Err: errors.New("not configured")}

// ClientConfig configures a TR-064 client.
type ClientConfig struct {
	// Host is the FRITZ!Box, e.g. "fritz.box" or "192.168.178.1".
	Host     string
	Username string
	Password string
	// PlainPort overrides 49000 (tests).
	PlainPort string
	// TLSConfig overrides the TLS settings (tests).
	TLSConfig *tls.Config
	Timeout   time.Duration
	Logger    *slog.Logger
}

// Client talks TR-064 to one FRITZ!Box.
type Client struct {
	cfg    ClientConfig
	plain  *http.Client
	secure *http.Client
	log    *slog.Logger

	mu           sync.Mutex
	securityPort string
}

// NewClient creates a client. It does not connect yet.
func NewClient(cfg ClientConfig) *Client {
	if cfg.PlainPort == "" {
		cfg.PlainPort = plainPort
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultHTTPTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	tlsConfig := cfg.TLSConfig
	if tlsConfig == nil {
		// The FRITZ!Box serves TR-064 over TLS with a self-signed
		// certificate for its own host name, so it cannot be verified
		// against a CA. TLS still keeps the phonebook and call list
		// away from passive listeners in the LAN; the login itself is
		// protected by HTTP digest authentication on top.
		tlsConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} //nolint:gosec // see above
	}
	return &Client{
		cfg:   cfg,
		plain: &http.Client{Timeout: cfg.Timeout},
		secure: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &digest.Transport{
				Username:  cfg.Username,
				Password:  cfg.Password,
				Transport: &http.Transport{TLSClientConfig: tlsConfig, MaxIdleConnsPerHost: 2, IdleConnTimeout: time.Minute},
			},
			// Downloads must not be redirected to another host.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		log: cfg.Logger.With("component", "fritzbox"),
	}
}

// secureBase returns https://host:<security port>, asking the FRITZ!Box for
// the port (DeviceInfo#GetSecurityPort, no login) the first time.
func (c *Client) secureBase(ctx context.Context) (string, error) {
	c.mu.Lock()
	port := c.securityPort
	c.mu.Unlock()
	if port == "" {
		plain := "http://" + net.JoinHostPort(c.cfg.Host, c.cfg.PlainPort)
		values, err := c.soap(ctx, c.plain, plain, deviceInfoControl, deviceInfoService, "GetSecurityPort", nil)
		if err != nil {
			return "", err
		}
		port = values["NewSecurityPort"]
		if port == "" {
			return "", &Error{Kind: KindProtocol, Err: errors.New("GetSecurityPort returned no port")}
		}
		c.mu.Lock()
		c.securityPort = port
		c.mu.Unlock()
	}
	return "https://" + net.JoinHostPort(c.cfg.Host, port), nil
}

// forgetSecurityPort makes the next call ask for the port again, e.g. after
// the FRITZ!Box restarted with another port.
func (c *Client) forgetSecurityPort() {
	c.mu.Lock()
	c.securityPort = ""
	c.mu.Unlock()
}

// call runs an authenticated TR-064 action over TLS.
func (c *Client) call(ctx context.Context, control, service, action string, args [][2]string) (map[string]string, error) {
	base, err := c.secureBase(ctx)
	if err != nil {
		return nil, err
	}
	values, err := c.soap(ctx, c.secure, base, control, service, action, args)
	var fe *Error
	if errors.As(err, &fe) && fe.Kind == KindUnreachable {
		c.forgetSecurityPort()
	}
	return values, err
}

func (c *Client) soap(ctx context.Context, client *http.Client, base, control, service, action string, args [][2]string) (map[string]string, error) {
	var body bytes.Buffer
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?>` +
		`<s:Envelope s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/" xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">` +
		`<s:Body><u:` + action + ` xmlns:u="` + service + `">`)
	for _, arg := range args {
		body.WriteString("<" + arg[0] + ">")
		_ = xml.EscapeText(&body, []byte(arg[1]))
		body.WriteString("</" + arg[0] + ">")
	}
	body.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+control, bytes.NewReader(body.Bytes()))
	if err != nil {
		return nil, &Error{Kind: KindProtocol, Err: err}
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+service+"#"+action+`"`)
	resp, err := client.Do(req)
	if err != nil {
		return nil, &Error{Kind: KindUnreachable, Err: fmt.Errorf("%s: %w", action, err)}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, &Error{Kind: KindUnreachable, Err: fmt.Errorf("%s: %w", action, err)}
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, &Error{Kind: KindAuth, Err: fmt.Errorf("%s: login rejected (HTTP 401)", action)}
	case resp.StatusCode != http.StatusOK:
		return nil, faultError(action, resp.StatusCode, data)
	}
	values, err := responseValues(data, action+"Response")
	if err != nil {
		return nil, &Error{Kind: KindProtocol, Err: fmt.Errorf("%s: %w", action, err)}
	}
	return values, nil
}

// faultError maps a SOAP fault. UPnP 606 = "Action not authorized" (user
// lacks the right), 820 = internal error (e.g. feature not supported).
func faultError(action string, status int, data []byte) error {
	var fault struct {
		Code        string `xml:"Body>Fault>detail>UPnPError>errorCode"`
		Description string `xml:"Body>Fault>detail>UPnPError>errorDescription"`
	}
	_ = newDecoder(bytes.NewReader(data)).Decode(&fault)
	err := fmt.Errorf("%s: HTTP %d, UPnP error %s %s", action, status, fault.Code, fault.Description)
	switch strings.TrimSpace(fault.Code) {
	case "401", "606":
		return &Error{Kind: KindAuth, Err: err}
	case "820":
		return &Error{Kind: KindUnsupported, Err: err}
	}
	return &Error{Kind: KindProtocol, Err: err}
}

// responseValues collects the child elements of <actionResponse>.
func responseValues(data []byte, element string) (map[string]string, error) {
	dec := newDecoder(bytes.NewReader(data))
	values := map[string]string{}
	inside, depth := false, 0
	var name string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case t.Name.Local == element:
				inside, depth = true, 0
			case inside:
				depth++
				if depth == 1 {
					name = t.Name.Local
					text.Reset()
				}
			}
		case xml.CharData:
			if inside && depth == 1 {
				text.Write(t)
			}
		case xml.EndElement:
			switch {
			case inside && t.Name.Local == element && depth == 0:
				return values, nil
			case inside:
				if depth == 1 {
					values[name] = strings.TrimSpace(text.String())
				}
				depth--
			}
		}
	}
	return nil, fmt.Errorf("no <%s> in response", element)
}

// download fetches a URL the FRITZ!Box returned (phonebook or call list).
// The URL carries a session ID; its host is replaced by the configured
// host and security port, so a "fritz.box" in the URL works even when the
// server resolves that name differently, and nothing is ever fetched from
// another host.
func (c *Client) download(ctx context.Context, raw string, extra url.Values) ([]byte, error) {
	base, err := c.secureBase(ctx)
	if err != nil {
		return nil, err
	}
	src, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || src.Path == "" {
		return nil, &Error{Kind: KindProtocol, Err: fmt.Errorf("invalid download URL %q", raw)}
	}
	dst, _ := url.Parse(base)
	dst.Path = src.Path
	query := src.Query()
	for k, v := range extra {
		query[k] = v
	}
	dst.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dst.String(), nil)
	if err != nil {
		return nil, &Error{Kind: KindProtocol, Err: err}
	}
	resp, err := c.secure.Do(req)
	if err != nil {
		c.forgetSecurityPort()
		return nil, &Error{Kind: KindUnreachable, Err: fmt.Errorf("download %s: %w", src.Path, err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &Error{Kind: KindAuth, Err: fmt.Errorf("download %s: HTTP %d", src.Path, resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{Kind: KindProtocol, Err: fmt.Errorf("download %s: HTTP %d", src.Path, resp.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, &Error{Kind: KindUnreachable, Err: fmt.Errorf("download %s: %w", src.Path, err)}
	}
	if len(data) > maxDownloadBytes {
		return nil, &Error{Kind: KindProtocol, Err: fmt.Errorf("download %s: larger than %d bytes", src.Path, maxDownloadBytes)}
	}
	return data, nil
}

// newDecoder accepts the ISO-8859-1 some FRITZ!OS versions declare.
func newDecoder(r io.Reader) *xml.Decoder {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(strings.TrimSpace(charset)) {
		case "utf-8", "utf8", "":
			return input, nil
		case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "windows-1252":
			return &latin1Reader{r: input}, nil
		}
		return nil, fmt.Errorf("unsupported charset %q", charset)
	}
	return dec
}

// latin1Reader converts ISO-8859-1 bytes to UTF-8.
type latin1Reader struct {
	r       io.Reader
	pending []byte
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	if len(l.pending) == 0 {
		raw := make([]byte, max(len(p), 1))
		read, err := l.r.Read(raw)
		for _, b := range raw[:read] {
			if b < 0x80 {
				l.pending = append(l.pending, b)
			} else {
				l.pending = append(l.pending, 0xC0|b>>6, 0x80|b&0x3F)
			}
		}
		if len(l.pending) == 0 {
			return 0, err
		}
	}
	n := copy(p, l.pending)
	l.pending = l.pending[n:]
	return n, nil
}
