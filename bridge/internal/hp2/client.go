package hp2

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// Client speaks HP2 as a device. The Apple apps implement the same in
// Swift; this Go client serves the probe phone and the tests.
type Client struct {
	// BaseURL is the bridge, e.g. https://phone.example.com or
	// http://127.0.0.1:8080 (the WebSocket URL is derived from it).
	BaseURL   string
	DeviceID  string
	BridgeID  string
	Key       DeviceKey
	BridgePub ed25519.PublicKey
	HTTP      *http.Client
	Now       func() time.Time
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Response is a verified and opened answer.
type Response struct {
	Status int
	Header http.Header
	// Body is the plaintext body (opened if it was sealed).
	Body []byte
}

// Do sends a signed request, checks the bridge's signature on the answer
// and opens a sealed body. An answer without a valid bridge signature is an
// error (ErrUntrustedBridge), whatever its status.
func (c *Client) Do(ctx context.Context, method, requestURI string, body []byte, header http.Header) (Response, error) {
	creq, err := NewClientRequest(c.Key, c.DeviceID, c.BridgeID, method, requestURI, body, c.now())
	if err != nil {
		return Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.BaseURL, "/")+requestURI, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Authorization", creq.Authorization())
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return Response{}, err
	}
	keys, err := creq.VerifyAnswer(c.BridgePub, res.StatusCode, raw, res.Header.Get(BridgeHeader))
	if err != nil {
		return Response{Status: res.StatusCode, Header: res.Header}, err
	}
	out := Response{Status: res.StatusCode, Header: res.Header, Body: raw}
	if res.Header.Get("Content-Type") == SealedContentType {
		plain, err := OpenBody(keys.BridgeToDevice, raw)
		if err != nil {
			return Response{Status: res.StatusCode, Header: res.Header}, err
		}
		out.Body = plain
	}
	return out, nil
}

// WebSocketURL derives ws(s)://host/v1/ws from BaseURL.
func (c *Client) WebSocketURL() string {
	base := strings.TrimSuffix(c.BaseURL, "/")
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	return base + "/v1/ws"
}

// Dial opens the sealed WebSocket (/v1/ws). The 101 answer must carry a
// valid bridge signature.
func (c *Client) Dial(ctx context.Context) (*Conn, error) {
	wsURL := c.WebSocketURL()
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	creq, err := NewClientRequest(c.Key, c.DeviceID, c.BridgeID, http.MethodGet, u.RequestURI(), nil, c.now())
	if err != nil {
		return nil, err
	}
	ws, res, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: c.HTTP,
		HTTPHeader: http.Header{"Authorization": {creq.Authorization()}},
	})
	if err != nil {
		if res != nil {
			return nil, &StatusError{Status: res.StatusCode, Err: err}
		}
		return nil, err
	}
	keys, err := creq.VerifyAnswer(c.BridgePub, res.StatusCode, nil, res.Header.Get(BridgeHeader))
	if err != nil {
		ws.CloseNow()
		return nil, err
	}
	ws.SetReadLimit(1 << 20)
	conn, err := NewConn(ws, keys.DeviceToBridge, keys.BridgeToDevice)
	if err != nil {
		ws.CloseNow()
		return nil, err
	}
	return conn, nil
}

// StatusError is a rejected WebSocket upgrade.
type StatusError struct {
	Status int
	Err    error
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("hp2: upgrade rejected with %d: %v", e.Status, e.Err)
}
func (e *StatusError) Unwrap() error { return e.Err }

// PairResult is what a device stores after pairing.
type PairResult struct {
	DeviceID   string
	BridgeID   string
	BridgeName string
	BridgePub  ed25519.PublicKey
}

// PairError is a rejected pairing (error payload from the bridge).
type PairError struct {
	Status int
	Code   string
	// Message is the bridge's message for the user.
	Message string
}

func (e *PairError) Error() string {
	return fmt.Sprintf("pairing rejected (%d %s): %s", e.Status, e.Code, e.Message)
}

// Pair runs POST /v1/pair against baseURL and verifies the answer against
// the fingerprint from the pairing link.
func Pair(ctx context.Context, httpClient *http.Client, baseURL string, key DeviceKey, code, fingerprint, deviceName, platform, model string) (PairResult, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	body, err := NewPairRequest(key, code, deviceName, platform, model)
	if err != nil {
		return PairResult{}, err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return PairResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(baseURL, "/")+"/v1/pair", bytes.NewReader(data))
	if err != nil {
		return PairResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return PairResult{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return PairResult{}, err
	}
	if res.StatusCode != http.StatusOK {
		var e protocol.Error
		_ = json.Unmarshal(raw, &e)
		return PairResult{}, &PairError{Status: res.StatusCode, Code: e.Code, Message: e.Message}
	}
	var pr protocol.PairResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return PairResult{}, err
	}
	pub, err := VerifyPairResponse(body, pr, fingerprint)
	if err != nil {
		return PairResult{}, err
	}
	return PairResult{DeviceID: pr.DeviceID, BridgeID: pr.BridgeID, BridgeName: pr.BridgeName, BridgePub: pub}, nil
}

// ErrNotPairingLink reports a link that is not a v2 pairing link.
var ErrNotPairingLink = errors.New("not a housephone v2 pairing link")

// PairingLink is a parsed housephone://pair?v=2&… link.
type PairingLink struct {
	URL         string
	Code        string
	Fingerprint string
	Name        string
}

// ParsePairingLink parses a v2 pairing link.
func ParsePairingLink(link string) (PairingLink, error) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "housephone" || u.Host != "pair" {
		return PairingLink{}, ErrNotPairingLink
	}
	q := u.Query()
	if q.Get("v") != "2" || q.Get("url") == "" || q.Get("code") == "" || q.Get("fp") == "" {
		return PairingLink{}, ErrNotPairingLink
	}
	return PairingLink{URL: q.Get("url"), Code: NormalizeCode(q.Get("code")), Fingerprint: q.Get("fp"), Name: q.Get("name")}, nil
}

// FormatPairingLink builds housephone://pair?v=2&url=…&code=…&fp=…&name=…
// in the order of the spec, percent-encoding spaces as %20. Readers must
// not depend on the order.
func FormatPairingLink(publicURL, code, fingerprint, bridgeName string) string {
	esc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return "housephone://pair?v=2&url=" + esc(publicURL) + "&code=" + esc(code) +
		"&fp=" + esc(fingerprint) + "&name=" + esc(bridgeName)
}

// HTTPBase derives the HTTPS base URL from the WebSocket URL of a pairing
// link (wss://host/v1/ws → https://host).
func HTTPBase(wsURL string) (string, error) {
	u, err := url.Parse(wsURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid bridge URL %q", wsURL)
	}
	switch u.Scheme {
	case "wss", "https":
		u.Scheme = "https"
	case "ws", "http":
		u.Scheme = "http"
	default:
		return "", fmt.Errorf("invalid bridge URL %q", wsURL)
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return u.String(), nil
}
