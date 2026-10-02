// Package apns sends VoIP pushes to Apple (token-based auth with a .p8 key).
// The bridge uses it when it has its own key, the push relay always.
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"
)

// Development selects the APNs sandbox; every other value means production.
const Development = "development"

// ErrInvalidToken means the device token must be discarded.
var ErrInvalidToken = errors.New("apns: device token invalid")

// Notification is one VoIP push.
type Notification struct {
	// Token is the device token in lower-case hex.
	Token       string
	Environment string
	// Topic is "<bundle id>.voip".
	Topic string
	// CollapseID replaces an earlier push with the same ID (at most 64
	// bytes); empty for none.
	CollapseID string
	Body       []byte
}

// Error is an APNs rejection.
type Error struct {
	Status int
	Reason string
}

func (e *Error) Error() string { return fmt.Sprintf("apns %d %s", e.Status, e.Reason) }

// Is makes errors.Is(err, ErrInvalidToken) true for a dead token.
func (e *Error) Is(target error) bool {
	if target != ErrInvalidToken {
		return false
	}
	if e.Status == http.StatusGone {
		return true
	}
	switch e.Reason {
	case apns2.ReasonBadDeviceToken, apns2.ReasonDeviceTokenNotForTopic, apns2.ReasonUnregistered:
		return true
	}
	return false
}

// Client sends to development (sandbox) or production devices.
//
// The request is built here instead of with apns2.Client.Push, because apns2
// cannot send "apns-expiration: 0" (deliver now or never), which is what a
// ringing call needs. apns2 still provides the JWT and the HTTP/2 client.
type Client struct {
	token       *token.Token
	development endpoint
	production  endpoint
}

type endpoint struct {
	host   string
	client *http.Client
}

// Load reads the .p8 key.
func Load(keyFile, keyID, teamID string) (*Client, error) {
	key, err := token.AuthKeyFromFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("load APNs key %s: %w", keyFile, err)
	}
	return New(key, keyID, teamID), nil
}

// New uses an already loaded key.
func New(key *ecdsa.PrivateKey, keyID, teamID string) *Client {
	tok := &token.Token{AuthKey: key, KeyID: keyID, TeamID: teamID}
	httpClient := apns2.NewTokenClient(tok).HTTPClient
	return &Client{
		token:       tok,
		development: endpoint{host: apns2.HostDevelopment, client: httpClient},
		production:  endpoint{host: apns2.HostProduction, client: httpClient},
	}
}

// SetEndpoints points both environments at a custom host (tests).
func (c *Client) SetEndpoints(host string, client *http.Client) {
	c.development = endpoint{host: host, client: client}
	c.production = endpoint{host: host, client: client}
}

// Send delivers one VoIP push now or never.
func (c *Client) Send(ctx context.Context, n Notification) error {
	ep := c.production
	if n.Environment == Development {
		ep = c.development
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.host+"/3/device/"+n.Token, bytes.NewReader(n.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "bearer "+c.token.GenerateIfExpired())
	req.Header.Set("apns-topic", n.Topic)
	req.Header.Set("apns-push-type", string(apns2.PushTypeVOIP))
	req.Header.Set("apns-priority", "10")
	req.Header.Set("apns-expiration", "0")
	if n.CollapseID != "" {
		req.Header.Set("apns-collapse-id", n.CollapseID)
	}

	res, err := ep.client.Do(req)
	if err != nil {
		return fmt.Errorf("apns: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	var body struct {
		Reason string `json:"reason"`
	}
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	_ = json.Unmarshal(data, &body)
	return &Error{Status: res.StatusCode, Reason: body.Reason}
}
