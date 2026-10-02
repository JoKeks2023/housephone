package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/apns"
)

// RelayPushPath is the relay endpoint (ADR-0010). The relay itself is run
// by the app's publisher and lives in its own repository.
const RelayPushPath = "/v1/push"

// RelayRequest asks the relay to wake one device.
type RelayRequest struct {
	// Token is the APNs device token, lower-case hex.
	Token string `json:"token"`
	// Environment is "production" or "development".
	Environment string `json:"environment"`
	// Topic is "<bundle id>.voip"; the relay serves only its app's topics.
	Topic string `json:"topic"`
	// CollapseID is the call ID (optional, at most 64 bytes).
	CollapseID string `json:"collapseId,omitempty"`
	// Sealed is the sealed payload (pushseal), base64url without padding.
	// The relay sends {"sealed": Sealed} to APNs.
	Sealed string `json:"sealed"`
}

// RelayResponse is the relay's answer: 200 sent, 400 malformed, 403 topic
// not served, 410 device token invalid, 413 too large, 429 rate limited,
// 502 APNs failed (with Reason).
type RelayResponse struct {
	Error  string `json:"error,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// RelayClient sends pushes through the push relay (ADR-0010).
type RelayClient struct {
	url    string
	client *http.Client
}

// NewRelayClient talks to the relay at base (https://…).
func NewRelayClient(base string, client *http.Client) *RelayClient {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &RelayClient{url: strings.TrimSuffix(base, "/") + RelayPushPath, client: client}
}

// Send implements Sender. n.Body must be a SealedPush.
func (c *RelayClient) Send(ctx context.Context, n apns.Notification) error {
	var sealed SealedPush
	if err := json.Unmarshal(n.Body, &sealed); err != nil || sealed.Sealed == "" {
		return errors.New("relay: only sealed payloads")
	}
	env := n.Environment
	if env != apns.Development {
		env = "production"
	}
	body, err := json.Marshal(RelayRequest{
		Token:       n.Token,
		Environment: env,
		Topic:       n.Topic,
		CollapseID:  n.CollapseID,
		Sealed:      sealed.Sealed,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("relay: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	var out RelayResponse
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	_ = json.Unmarshal(data, &out)
	if res.StatusCode == http.StatusGone {
		// The relay already decided the token is dead.
		return &apns.Error{Status: http.StatusGone, Reason: out.Reason}
	}
	msg := out.Error
	if out.Reason != "" {
		msg += " (" + out.Reason + ")"
	}
	return fmt.Errorf("relay %d: %s", res.StatusCode, msg)
}
