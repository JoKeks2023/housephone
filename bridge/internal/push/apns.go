// Package push sends VoIP pushes via APNs (token-based auth with a .p8 key).
package push

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

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Config holds the APNs credentials.
type Config struct {
	KeyFile string
	KeyID   string
	TeamID  string
	// Topic is "<bundle id>.voip".
	Topic string
}

// APNs sends VoIP pushes to development (sandbox) or production devices.
//
// The request is built here instead of with apns2.Client.Push, because apns2
// cannot send "apns-expiration: 0" (deliver now or never), which is what a
// ringing call needs. apns2 still provides the JWT and the HTTP/2 client.
type APNs struct {
	topic       string
	token       *token.Token
	development endpoint
	production  endpoint
}

type endpoint struct {
	host   string
	client *http.Client
}

// NewAPNs loads the .p8 key and prepares both environments.
func NewAPNs(cfg Config) (*APNs, error) {
	key, err := token.AuthKeyFromFile(cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load APNs key %s: %w", cfg.KeyFile, err)
	}
	return NewAPNsWithKey(key, cfg), nil
}

// NewAPNsWithKey is NewAPNs with an already loaded key.
func NewAPNsWithKey(key *ecdsa.PrivateKey, cfg Config) *APNs {
	tok := &token.Token{AuthKey: key, KeyID: cfg.KeyID, TeamID: cfg.TeamID}
	httpClient := apns2.NewTokenClient(tok).HTTPClient
	return &APNs{
		topic:       cfg.Topic,
		token:       tok,
		development: endpoint{host: apns2.HostDevelopment, client: httpClient},
		production:  endpoint{host: apns2.HostProduction, client: httpClient},
	}
}

// SetEndpoints points both environments at a custom host (tests).
func (a *APNs) SetEndpoints(host string, client *http.Client) {
	a.development = endpoint{host: host, client: client}
	a.production = endpoint{host: host, client: client}
}

// PushIncomingCall implements calls.Pusher.
func (a *APNs) PushIncomingCall(ctx context.Context, dev store.Device, payload protocol.PushIncomingCall) error {
	if dev.PushToken == "" {
		return fmt.Errorf("device %s has no push token", dev.ID)
	}
	ep := a.production
	if dev.PushEnvironment == protocol.PushEnvironmentDevelopment {
		ep = a.development
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.host+"/3/device/"+dev.PushToken, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "bearer "+a.token.GenerateIfExpired())
	topic := a.topic
	if dev.PushTopic != "" {
		// Per-device topic (v1.1), e.g. the watch app. Validated when stored.
		topic = dev.PushTopic
	}
	req.Header.Set("apns-topic", topic)
	req.Header.Set("apns-push-type", string(apns2.PushTypeVOIP))
	req.Header.Set("apns-priority", "10")
	req.Header.Set("apns-expiration", "0")
	req.Header.Set("apns-collapse-id", payload.CallID)

	res, err := ep.client.Do(req)
	if err != nil {
		return fmt.Errorf("apns: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	var apnsErr struct {
		Reason string `json:"reason"`
	}
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	_ = json.Unmarshal(data, &apnsErr)
	err = fmt.Errorf("apns %d %s", res.StatusCode, apnsErr.Reason)
	if invalidToken(res.StatusCode, apnsErr.Reason) {
		return errors.Join(err, calls.ErrInvalidPushToken)
	}
	return err
}

func invalidToken(status int, reason string) bool {
	if status == http.StatusGone {
		return true
	}
	switch reason {
	case apns2.ReasonBadDeviceToken, apns2.ReasonDeviceTokenNotForTopic, apns2.ReasonUnregistered:
		return true
	}
	return false
}
