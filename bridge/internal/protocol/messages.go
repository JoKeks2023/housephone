// Package protocol defines the Housephone signaling protocol v1 with the
// v1.1 watch extension (docs/protocol/signaling-v1.md) shared between the
// bridge and devices.
package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// Message types sent by devices.
const (
	TypePair         = "pair"
	TypeHello        = "hello"
	TypeDeviceUpdate = "device.update"
	TypeDeviceUnpair = "device.unpair"
	TypeCallAttach   = "call.attach"
	TypeCallDial     = "call.dial"
	TypeCallAnswer   = "call.answer"
	TypeCallAccept   = "call.accept"
	TypeCallHangup   = "call.hangup"
	TypeCallDTMF     = "call.dtmf"
	// TypePairCompanionRequest asks for a pairing code for a companion
	// device such as the watch (v1.1).
	TypePairCompanionRequest = "pair.companion.request"
)

// Message types sent by the bridge.
const (
	TypePairOK       = "pair.ok"
	TypeWelcome      = "welcome"
	TypeStatus       = "status"
	TypeCallIncoming = "call.incoming"
	TypeCallOffer    = "call.offer"
	TypeCallState    = "call.state"
	TypeCallEnded    = "call.ended"
	TypeError        = "error"
	// TypePairCompanion answers pair.companion.request (v1.1).
	TypePairCompanion = "pair.companion"
	// TypeCallMedia replaces call.offer for websocket-pcma devices (v1.1).
	TypeCallMedia = "call.media"
)

// Media capabilities in hello/device.update (v1.1).
const (
	MediaWebRTC        = "webrtc"
	MediaWebSocketPCMA = "websocket-pcma"
)

// Binary audio frames of the websocket-pcma media path (v1.1): one type
// byte followed by 20 ms of A-law at 8 kHz.
const (
	AudioFrameType  byte = 0x01
	AudioFrameBytes      = 160
	AudioFrameLen        = 1 + AudioFrameBytes
)

// Parameters announced in call.media (v1.1).
const (
	MediaTransportWebSocket = "websocket"
	MediaSampleRate         = 8000
	MediaFrameMs            = 20
)

// Platforms.
const (
	PlatformIOS     = "ios"
	PlatformWatchOS = "watchos"
)

// Push environments.
const (
	PushEnvironmentDevelopment = "development"
	PushEnvironmentProduction  = "production"
)

// Hangup reasons sent by devices.
const (
	HangupReasonHangup   = "hangup"
	HangupReasonDeclined = "declined"
	HangupReasonFailed   = "failed"
)

// Call states in call.state.
const (
	CallStateRinging    = "ringing"
	CallStateEarlyMedia = "early_media"
	CallStateConnected  = "connected"
)

// End reasons in call.ended.
const (
	EndReasonRemoteHangup      = "remote_hangup"
	EndReasonRemoteCancelled   = "remote_cancelled"
	EndReasonAnsweredElsewhere = "answered_elsewhere"
	EndReasonDeclinedElsewhere = "declined_elsewhere"
	EndReasonBusy              = "busy"
	EndReasonRejected          = "rejected"
	EndReasonNotFound          = "not_found"
	EndReasonFailed            = "failed"
	EndReasonLocalHangup       = "local_hangup"
)

// Error codes in error.
const (
	ErrorUnauthorized       = "unauthorized"
	ErrorBadRequest         = "bad_request"
	ErrorPairingInvalid     = "pairing_invalid"
	ErrorPairingRateLimited = "pairing_rate_limited"
	ErrorSIPUnavailable     = "sip_unavailable"
	ErrorCallNotFound       = "call_not_found"
	ErrorInvalidNumber      = "invalid_number"
	ErrorInternal           = "internal"
)

// WebSocket close codes beyond RFC 6455.
const (
	// CloseReplaced is used when a newer connection of the same device
	// replaces this one.
	CloseReplaced = 4001
)

// Envelope is a single WebSocket text message.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// NewEnvelope marshals payload into an envelope of the given type.
func NewEnvelope(msgType string, payload any) (Envelope, error) {
	if payload == nil {
		payload = struct{}{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal %s payload: %w", msgType, err)
	}
	return Envelope{Type: msgType, Payload: raw}, nil
}

// MustEnvelope is NewEnvelope for payloads that cannot fail to marshal.
func MustEnvelope(msgType string, payload any) Envelope {
	env, err := NewEnvelope(msgType, payload)
	if err != nil {
		panic(err)
	}
	return env
}

// Decode unmarshals the payload into v. An absent payload decodes as {}.
func (e Envelope) Decode(v any) error {
	raw := e.Payload
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("decode %s payload: %w", e.Type, err)
	}
	return nil
}

// Marshal encodes the envelope as a WebSocket text message.
func (e Envelope) Marshal() ([]byte, error) {
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage("{}")
	}
	return json.Marshal(e)
}

// ParseEnvelope decodes a WebSocket text message.
func ParseEnvelope(data []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, fmt.Errorf("parse message: %w", err)
	}
	if env.Type == "" {
		return Envelope{}, fmt.Errorf("parse message: missing type")
	}
	return env, nil
}

// Device → bridge payloads.

type Pair struct {
	Code       string `json:"code"`
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
	Model      string `json:"model,omitempty"`
}

type Hello struct {
	AppVersion      string `json:"appVersion"`
	Platform        string `json:"platform"`
	PushToken       string `json:"pushToken,omitempty"`
	PushEnvironment string `json:"pushEnvironment,omitempty"`
	// MediaCapabilities (v1.1); absent means ["webrtc"].
	MediaCapabilities []string `json:"mediaCapabilities,omitempty"`
	// PushTopic (v1.1); absent means the bridge's configured topic.
	PushTopic string `json:"pushTopic,omitempty"`
}

type DeviceUpdate struct {
	PushToken       *string `json:"pushToken,omitempty"`
	PushEnvironment *string `json:"pushEnvironment,omitempty"`
	DeviceName      *string `json:"deviceName,omitempty"`
	// MediaCapabilities (v1.1); nil leaves the stored value unchanged.
	MediaCapabilities []string `json:"mediaCapabilities,omitempty"`
	// PushTopic (v1.1); nil leaves the stored value unchanged.
	PushTopic *string `json:"pushTopic,omitempty"`
}

// PairCompanionRequest asks for a pairing code for another device of the
// same user, such as the watch (v1.1).
type PairCompanionRequest struct {
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
}

// DeviceUnpair asks the bridge to forget the device. It has no fields.
type DeviceUnpair struct{}

type CallAttach struct {
	CallID string `json:"callId"`
}

type CallDial struct {
	CallID string `json:"callId"`
	Number string `json:"number"`
}

type CallAnswer struct {
	CallID string `json:"callId"`
	SDP    string `json:"sdp"`
}

type CallAccept struct {
	CallID string `json:"callId"`
}

type CallHangup struct {
	CallID string `json:"callId"`
	Reason string `json:"reason,omitempty"`
}

type CallDTMF struct {
	CallID string `json:"callId"`
	Digits string `json:"digits"`
}

// Bridge → device payloads.

type PairOK struct {
	DeviceID     string `json:"deviceId"`
	DeviceSecret string `json:"deviceSecret"`
	BridgeID     string `json:"bridgeId"`
	BridgeName   string `json:"bridgeName"`
}

type Welcome struct {
	BridgeID      string `json:"bridgeId"`
	BridgeName    string `json:"bridgeName"`
	BridgeVersion string `json:"bridgeVersion"`
	SIPRegistered bool   `json:"sipRegistered"`
}

type Status struct {
	SIPRegistered bool `json:"sipRegistered"`
}

type CallIncoming struct {
	CallID     string    `json:"callId"`
	Caller     string    `json:"caller"`
	CallerName string    `json:"callerName,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
}

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type CallOffer struct {
	CallID     string      `json:"callId"`
	SDP        string      `json:"sdp"`
	ICEServers []ICEServer `json:"iceServers"`
}

type CallState struct {
	CallID string `json:"callId"`
	State  string `json:"state"`
}

type CallEnded struct {
	CallID  string `json:"callId"`
	Reason  string `json:"reason"`
	SIPCode int    `json:"sipCode,omitempty"`
}

// Call status states in GET /v1/calls/{callId} (v1.1).
const (
	CallStatusRinging   = "ringing"
	CallStatusConnected = "connected"
	CallStatusEnded     = "ended"
)

// CallStatus answers GET /v1/calls/{callId} from the requesting device's
// point of view (v1.1). A ringing watch may not open a WebSocket, so it
// polls this to learn about CANCEL or an answer elsewhere.
type CallStatus struct {
	CallID  string `json:"callId"`
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	SIPCode int    `json:"sipCode,omitempty"`
}

// PairCompanion carries a fresh pairing code for a companion device (v1.1).
type PairCompanion struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// CallMedia tells a websocket-pcma device how audio flows (v1.1).
type CallMedia struct {
	CallID     string `json:"callId"`
	Transport  string `json:"transport"`
	Codec      string `json:"codec"`
	SampleRate int    `json:"sampleRate"`
	FrameMs    int    `json:"frameMs"`
}

// NewCallMedia describes the websocket-pcma media path of a call.
func NewCallMedia(callID string) CallMedia {
	return CallMedia{CallID: callID, Transport: MediaTransportWebSocket, Codec: "PCMA", SampleRate: MediaSampleRate, FrameMs: MediaFrameMs}
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	CallID  string `json:"callId,omitempty"`
}

// PushIncomingCall is the APNs VoIP push payload. It is not a WebSocket
// message.
type PushIncomingCall struct {
	Type       string `json:"type"`
	V          int    `json:"v"`
	CallID     string `json:"callId"`
	Caller     string `json:"caller"`
	CallerName string `json:"callerName,omitempty"`
	BridgeID   string `json:"bridgeId"`
}

// Timestamp normalizes t for the wire: UTC without fractional seconds, so
// every ISO-8601 parser (including Swift's default one) accepts it.
func Timestamp(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second)
}

// PushTypeIncomingCall is the type field of PushIncomingCall.
const PushTypeIncomingCall = "incoming_call"

// NewPushIncomingCall builds a push payload for an incoming call.
func NewPushIncomingCall(callID, caller, callerName, bridgeID string) PushIncomingCall {
	return PushIncomingCall{
		Type:       PushTypeIncomingCall,
		V:          1,
		CallID:     callID,
		Caller:     caller,
		CallerName: callerName,
		BridgeID:   bridgeID,
	}
}
