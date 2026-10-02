package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// CloudflareTunnelsURL opens the tunnels of the signed-in Cloudflare
// account; ":account" lets the dashboard pick (or ask for) the account.
const CloudflareTunnelsURL = "https://dash.cloudflare.com/?to=/:account/tunnels"

// remoteState is what the "from anywhere" card shows.
type remoteState int

const (
	remoteMissing  remoteState = iota // no public address set
	remoteChecking                    // first check still running
	remoteOK                          // /v1/health answered through the public address
	remoteFailed                      // it did not
)

type remoteResult struct {
	State remoteState
	// Host is the public hostname; Error says why the check failed.
	Host  string
	Error string
}

// remoteCheck asks the bridge's own public address for /v1/health, the way
// a device from outside would, so the dashboard can say whether the tunnel
// works. The check runs in the background; pages show the last result.
type remoteCheck struct {
	client *http.Client
	ttl    time.Duration
	now    func() time.Time

	mu      sync.Mutex
	url     string
	at      time.Time
	result  remoteResult
	running bool
}

func newRemoteCheck() *remoteCheck {
	return &remoteCheck{client: &http.Client{Timeout: 5 * time.Second}, ttl: time.Minute, now: time.Now}
}

// Result returns the last result for publicURL and starts a new check when
// it is stale or the address changed.
func (c *remoteCheck) Result(publicURL string) remoteResult {
	health, host, err := healthURL(publicURL)
	if publicURL == "" {
		return remoteResult{State: remoteMissing}
	}
	if err != nil {
		return remoteResult{State: remoteFailed, Host: host, Error: err.Error()}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.url != publicURL {
		c.url, c.at, c.result = publicURL, time.Time{}, remoteResult{State: remoteChecking, Host: host}
	}
	if !c.running && c.now().Sub(c.at) >= c.ttl {
		c.running = true
		go c.run(publicURL, health, host)
	}
	return c.result
}

func (c *remoteCheck) run(publicURL, health, host string) {
	res := remoteResult{State: remoteOK, Host: host}
	if err := c.probe(health); err != nil {
		res = remoteResult{State: remoteFailed, Host: host, Error: err.Error()}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = false
	if c.url == publicURL {
		c.at, c.result = c.now(), res
	}
}

func (c *remoteCheck) probe(health string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, health, nil)
	if err != nil {
		return err
	}
	res, err := c.client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(http.MaxBytesReader(nil, res.Body, 4<<10)).Decode(&body) != nil || body.Status != "ok" {
		return errors.New("answer is not from the bridge")
	}
	return nil
}

// healthURL turns the public wss:// address into its https:// health URL.
func healthURL(publicURL string) (health, host string, err error) {
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" {
		return "", "", errors.New("invalid address")
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	default:
		return "", u.Host, errors.New("invalid address")
	}
	// /v1/ws → /v1/health, also under a path prefix of a proxy.
	if prefix, ok := strings.CutSuffix(u.Path, "/ws"); ok {
		u.Path = prefix + "/health"
	} else {
		u.Path = "/v1/health"
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String(), u.Hostname(), nil
}
