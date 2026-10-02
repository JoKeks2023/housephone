// Package push wakes devices for incoming calls with a VoIP push: directly
// with the bridge's own APNs key, or through the push relay (ADR-0010).
package push

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/JoKeks2023/housephone/bridge/internal/apns"
	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/pushseal"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// ErrNoPushKey means the device cannot receive sealed pushes (an app older
// than ADR-0010), which the relay requires.
var ErrNoPushKey = errors.New("device has no push key; update the app")

// Sender delivers one notification: an apns.Client or a RelayClient.
type Sender interface {
	Send(ctx context.Context, n apns.Notification) error
}

// Pusher implements calls.Pusher.
type Pusher struct {
	sender Sender
	// topic is used for devices that did not report their own.
	topic string
	// sealedOnly refuses plaintext payloads (relay).
	sealedOnly bool
}

// NewDirect sends with the bridge's own APNs key. Devices with a push key
// get sealed payloads, older apps plaintext.
func NewDirect(client *apns.Client, topic string) *Pusher {
	return &Pusher{sender: client, topic: topic}
}

// NewRelayed sends through the push relay, sealed payloads only.
func NewRelayed(relay *RelayClient, topic string) *Pusher {
	return &Pusher{sender: relay, topic: topic, sealedOnly: true}
}

// PushIncomingCall implements calls.Pusher.
func (p *Pusher) PushIncomingCall(ctx context.Context, dev store.Device, payload protocol.PushIncomingCall) error {
	if dev.PushToken == "" {
		return fmt.Errorf("device %s has no push token", dev.ID)
	}
	body, err := p.body(dev, payload)
	if err != nil {
		return err
	}
	topic := p.topic
	if dev.PushTopic != "" {
		// Per-device topic (v1.1), e.g. the watch app. Validated when stored.
		topic = dev.PushTopic
	}
	err = p.sender.Send(ctx, apns.Notification{
		Token:       dev.PushToken,
		Environment: dev.PushEnvironment,
		Topic:       topic,
		CollapseID:  payload.CallID,
		Body:        body,
	})
	if errors.Is(err, apns.ErrInvalidToken) {
		return errors.Join(err, calls.ErrInvalidPushToken)
	}
	return err
}

// SealedPush is the APNs body of a sealed push: the PushIncomingCall
// encrypted for the device's push key (pushseal), base64url without
// padding.
type SealedPush struct {
	Sealed string `json:"sealed"`
}

func (p *Pusher) body(dev store.Device, payload protocol.PushIncomingCall) ([]byte, error) {
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if dev.PushKey == "" {
		if p.sealedOnly {
			return nil, fmt.Errorf("device %s: %w", dev.ID, ErrNoPushKey)
		}
		return plain, nil
	}
	key, err := base64.RawURLEncoding.DecodeString(dev.PushKey)
	if err != nil {
		return nil, fmt.Errorf("device %s: %w", dev.ID, pushseal.ErrBadKey)
	}
	sealed, err := pushseal.Seal(rand.Reader, key, plain)
	if err != nil {
		return nil, fmt.Errorf("device %s: %w", dev.ID, err)
	}
	return json.Marshal(SealedPush{Sealed: base64.RawURLEncoding.EncodeToString(sealed)})
}
