package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// ErrNotRunning means no bridge serves the admin socket.
var ErrNotRunning = errors.New("die Bridge läuft nicht (kein Admin-Socket)")

// Client talks to a running bridge over the admin socket.
type Client struct {
	http *http.Client
}

// Dial returns a client if a bridge serves the socket in dataDir.
func Dial(dataDir string) (*Client, error) {
	path := filepath.Join(dataDir, SocketName)
	if _, err := os.Stat(path); err != nil {
		return nil, ErrNotRunning
	}
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return nil, ErrNotRunning
	}
	c.Close()
	return NewClient(func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", path)
	}), nil
}

// NewClient uses dial for every connection (tests pass an in-memory one).
func NewClient(dial func(ctx context.Context) (net.Conn, error)) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) }}
	return &Client{http: &http.Client{Transport: tr, Timeout: 90 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://admin"+path, rd)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if resp.StatusCode == http.StatusBadRequest && e.Error == ErrUnknownProfile.Error() {
			return ErrUnknownProfile
		}
		return fmt.Errorf("admin: %s", e.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Status(ctx context.Context) (s Status, err error) {
	return s, c.do(ctx, http.MethodGet, "/v1/status", nil, &s)
}

func (c *Client) Devices(ctx context.Context) (d []DeviceInfo, err error) {
	return d, c.do(ctx, http.MethodGet, "/v1/devices", nil, &d)
}

func (c *Client) RenameDevice(ctx context.Context, id, name string) (d DeviceInfo, err error) {
	return d, c.do(ctx, http.MethodPost, "/v1/devices/"+url.PathEscape(id)+"/rename", map[string]string{"name": name}, &d)
}

func (c *Client) RemoveDevice(ctx context.Context, id string, keepCompanions bool) (r RemoveResult, err error) {
	q := ""
	if keepCompanions {
		q = "?keepCompanions=1"
	}
	return r, c.do(ctx, http.MethodDelete, "/v1/devices/"+url.PathEscape(id)+q, nil, &r)
}

// CreatePairing creates a code whose device joins profile ("": default).
func (c *Client) CreatePairing(ctx context.Context, name, profile string) (p PairingInfo, err error) {
	return p, c.do(ctx, http.MethodPost, "/v1/pairing", map[string]string{"name": name, "profile": profile}, &p)
}

func (c *Client) Profiles(ctx context.Context) (p []ProfileInfo, err error) {
	return p, c.do(ctx, http.MethodGet, "/v1/profiles", nil, &p)
}

func (c *Client) MoveDevice(ctx context.Context, id, profile string) (r MoveResult, err error) {
	return r, c.do(ctx, http.MethodPost, "/v1/devices/"+url.PathEscape(id)+"/move", map[string]string{"profile": profile}, &r)
}

// WaitPairing long-polls once (up to ~25 s).
func (c *Client) WaitPairing(ctx context.Context, code string) (s PairingState, err error) {
	return s, c.do(ctx, http.MethodGet, "/v1/pairing/"+url.PathEscape(code)+"?wait=25", nil, &s)
}

func (c *Client) RevokePairing(ctx context.Context, code string) error {
	return c.do(ctx, http.MethodDelete, "/v1/pairing/"+url.PathEscape(code), nil, nil)
}

func (c *Client) LanPairings(ctx context.Context) (l []LanPairingRequest, err error) {
	return l, c.do(ctx, http.MethodGet, "/v1/lan-pairings", nil, &l)
}

// ApproveLanPairing pairs the device into profile ("": default).
func (c *Client) ApproveLanPairing(ctx context.Context, id, profile string) (d DeviceInfo, err error) {
	return d, c.do(ctx, http.MethodPost, "/v1/lan-pairings/"+url.PathEscape(id)+"/approve", map[string]string{"profile": profile}, &d)
}

func (c *Client) DenyLanPairing(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/lan-pairings/"+url.PathEscape(id)+"/deny", nil, nil)
}

func (c *Client) Calls(ctx context.Context) (v CallsView, err error) {
	return v, c.do(ctx, http.MethodGet, "/v1/calls", nil, &v)
}

func (c *Client) Stats(ctx context.Context) (s Stats, err error) {
	return s, c.do(ctx, http.MethodGet, "/v1/stats", nil, &s)
}

func (c *Client) Logs(ctx context.Context, after uint64) (v LogsView, err error) {
	return v, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/logs?after=%d", after), nil, &v)
}

func (c *Client) SelfTest(ctx context.Context) (checks []Check, err error) {
	return checks, c.do(ctx, http.MethodGet, "/v1/selftest", nil, &checks)
}

func (c *Client) Config(ctx context.Context) (m map[string]any, err error) {
	return m, c.do(ctx, http.MethodGet, "/v1/config", nil, &m)
}
