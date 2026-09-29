// Package calls orchestrates calls between the FRITZ!Box SIP leg and device
// legs (WebRTC, or binary WebSocket audio for the watch), following
// docs/protocol/signaling-v1.md.
//
// The Manager depends only on the interfaces in this file, so it can be
// tested with fakes. Production implementations live in internal/sipleg,
// internal/media, internal/push and internal/signaling.
package calls

import (
	"context"
	"errors"
	"fmt"

	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// SIPMedia is the RTP stream to and from the FRITZ!Box for one call.
type SIPMedia interface {
	// ReadRTP blocks for the next packet. It returns an error once the media
	// is closed.
	ReadRTP(buf []byte, pkt *rtp.Packet) (int, error)
	WriteRTP(pkt *rtp.Packet) error
	// SendDTMF sends RFC 4733 events for digits (0-9 * #).
	SendDTMF(digits string) error
}

// SIPEndReason tells why a SIP dialog ended.
type SIPEndReason int

const (
	SIPEndUnknown SIPEndReason = iota
	// SIPEndRemoteHangup: BYE from the FRITZ!Box.
	SIPEndRemoteHangup
	// SIPEndCancelled: CANCEL before answer.
	SIPEndCancelled
	// SIPEndAnsweredElsewhere: CANCEL with "Reason: SIP;cause=200".
	SIPEndAnsweredElsewhere
	// SIPEndFailed: transport error or timeout.
	SIPEndFailed
)

// IncomingSIPCall is an INVITE from the FRITZ!Box.
type IncomingSIPCall interface {
	Caller() string
	CallerName() string
	// Codec is the preferred pass-through codec of the INVITE offer
	// (G722 > PCMA > PCMU); WebRTC devices are offered this codec.
	Codec() codec.Codec
	// Offers reports whether the INVITE offer contains c.
	Offers(c codec.Codec) bool
	// Ringing sends 180 Ringing.
	Ringing() error
	// Answer sends 200 OK with codec c, which must be offered, and blocks
	// until ACK. The codec is chosen by the device that accepts (v1.1).
	Answer(ctx context.Context, c codec.Codec) (SIPMedia, error)
	// Reject sends a final error response before answer.
	Reject(status int, reason string) error
	// Hangup sends BYE after answer.
	Hangup(ctx context.Context) error
	// Done is closed when the dialog ends for any reason.
	Done() <-chan struct{}
	// EndReason is valid after Done is closed.
	EndReason() SIPEndReason
}

// OutgoingSIPCall is an answered outgoing call.
type OutgoingSIPCall interface {
	// Hangup sends BYE.
	Hangup(ctx context.Context) error
	// Done is closed when the dialog ends (e.g. BYE from the FRITZ!Box).
	Done() <-chan struct{}
}

// DialEvents are called from the dialing goroutine.
type DialEvents struct {
	// OnRinging: 180 Ringing without SDP.
	OnRinging func()
	// OnEarlyMedia: 183 Session Progress with SDP; audio flows from now on.
	OnEarlyMedia func(SIPMedia)
}

// DialError is a final SIP failure response to an outgoing INVITE.
type DialError struct {
	Status int
	Reason string
}

func (e *DialError) Error() string {
	return fmt.Sprintf("sip %d %s", e.Status, e.Reason)
}

// SIPLeg is the registration at the FRITZ!Box.
type SIPLeg interface {
	Registered() bool
	// Dial sends an INVITE offering only codec c and blocks until the call
	// is answered or fails. Cancelling ctx before the answer sends CANCEL.
	// On success the returned media is the same object passed to
	// OnEarlyMedia, if early media happened.
	Dial(ctx context.Context, number string, c codec.Codec, ev DialEvents) (OutgoingSIPCall, SIPMedia, error)
}

// PeerState is the connection state of a device's WebRTC peer.
type PeerState int

const (
	PeerNew PeerState = iota
	PeerConnected
	PeerDisconnected
	PeerFailed
	PeerClosed
)

func (s PeerState) String() string {
	switch s {
	case PeerNew:
		return "new"
	case PeerConnected:
		return "connected"
	case PeerDisconnected:
		return "disconnected"
	case PeerFailed:
		return "failed"
	case PeerClosed:
		return "closed"
	}
	return "unknown"
}

// Peer is the WebRTC connection to one device for one call. The bridge is
// always the offerer and does not trickle ICE.
type Peer interface {
	// CreateOffer returns a complete SDP offer (gathering finished).
	CreateOffer(ctx context.Context, iceRestart bool) (string, error)
	SetAnswer(sdp string) error
	// Codec is the audio codec the device selected in its answer.
	Codec() (codec.Codec, error)
	// ReadRTP blocks until the device's audio track delivers a packet.
	ReadRTP() (*rtp.Packet, error)
	// WriteRTP sends a packet to the device (SSRC/payload type are set by the
	// peer).
	WriteRTP(*rtp.Packet) error
	Close() error
}

// PeerFactory creates device peers.
type PeerFactory interface {
	// NewPeer offers codecs in the given order. onState must not block.
	NewPeer(codecs []codec.Codec, onState func(PeerState)) (Peer, error)
}

// AudioSink receives binary audio frames from a websocket-pcma device (v1.1).
type AudioSink interface {
	DeviceAudio(frame []byte)
}

// DeviceConn is an authenticated WebSocket connection of one device.
type DeviceConn interface {
	DeviceID() string
	// Send queues a message without blocking.
	Send(env protocol.Envelope)
	// SendAudio queues a binary audio frame without blocking. Frames are
	// dropped (not the connection) when the device cannot keep up (v1.1).
	SendAudio(frame []byte)
	// SetAudioSink routes the device's binary frames to sink (v1.1).
	SetAudioSink(sink AudioSink)
	// RemoveAudioSink clears the sink if it is still sink.
	RemoveAudioSink(sink AudioSink)
}

// ErrInvalidPushToken means the device's push token must be discarded.
var ErrInvalidPushToken = errors.New("push token invalid")

// Pusher sends VoIP pushes.
type Pusher interface {
	PushIncomingCall(ctx context.Context, dev store.Device, payload protocol.PushIncomingCall) error
}

// DeviceDirectory lists paired devices.
type DeviceDirectory interface {
	List() ([]store.Device, error)
	Get(id string) (store.Device, error)
	ClearPushToken(id, token string) error
}
