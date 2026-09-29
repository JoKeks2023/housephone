// Package testdevice emulates a device's WebRTC side (as the iOS app does it
// with libwebrtc) for tests: it answers the bridge's offer and exchanges RTP.
package testdevice

import (
	"context"
	"fmt"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
)

// Peer is the device side of a call.
type Peer struct {
	PC     *webrtc.PeerConnection
	Track  *webrtc.TrackLocalStaticRTP
	Remote chan *webrtc.TrackRemote
	State  chan webrtc.PeerConnectionState
}

// NewPeer creates a device peer that supports the given codecs, in order of
// preference. Loopback candidates are included so tests run on one host.
func NewPeer(codecs ...codec.Codec) (*Peer, error) {
	me := &webrtc.MediaEngine{}
	for _, c := range codecs {
		err := me.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: c.MimeType(), ClockRate: codec.ClockRate},
			PayloadType:        webrtc.PayloadType(c.PayloadType()),
		}, webrtc.RTPCodecTypeAudio)
		if err != nil {
			return nil, err
		}
	}
	se := webrtc.SettingEngine{}
	se.SetIncludeLoopbackCandidate(true)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	api := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: codecs[0].MimeType(), ClockRate: codec.ClockRate}, "mic", "device")
	if err != nil {
		return nil, err
	}
	if _, err := pc.AddTrack(track); err != nil {
		return nil, err
	}
	p := &Peer{PC: pc, Track: track, Remote: make(chan *webrtc.TrackRemote, 1), State: make(chan webrtc.PeerConnectionState, 16)}
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		select {
		case p.Remote <- t:
		default:
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		select {
		case p.State <- s:
		default:
		}
	})
	return p, nil
}

// Answer applies the offer and returns a complete answer.
func (p *Peer) Answer(ctx context.Context, offer string) (string, error) {
	if err := p.PC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return "", fmt.Errorf("set offer: %w", err)
	}
	answer, err := p.PC.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	gathered := webrtc.GatheringCompletePromise(p.PC)
	if err := p.PC.SetLocalDescription(answer); err != nil {
		return "", err
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return p.PC.LocalDescription().SDP, nil
}

// WaitConnected blocks until the peer connection is connected.
func (p *Peer) WaitConnected(timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case s := <-p.State:
			switch s {
			case webrtc.PeerConnectionStateConnected:
				return nil
			case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
				return fmt.Errorf("peer %s", s)
			}
		case <-deadline:
			return fmt.Errorf("not connected after %s", timeout)
		}
	}
}

// SendAudio writes n packets with the given payload every 20 ms.
func (p *Peer) SendAudio(ctx context.Context, n int, payload []byte) {
	for i := range n {
		pkt := &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(1000 + i), Timestamp: uint32(i * codec.SamplesPerPacket), SSRC: 0xD5}, Payload: payload}
		_ = p.Track.WriteRTP(pkt)
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// ReadAudio waits for the remote track and reads one packet.
func (p *Peer) ReadAudio(timeout time.Duration) (*rtp.Packet, *webrtc.TrackRemote, error) {
	var remote *webrtc.TrackRemote
	select {
	case remote = <-p.Remote:
		p.Remote <- remote
	case <-time.After(timeout):
		return nil, nil, fmt.Errorf("no remote track after %s", timeout)
	}
	type result struct {
		pkt *rtp.Packet
		err error
	}
	ch := make(chan result, 1)
	go func() {
		pkt, _, err := remote.ReadRTP()
		ch <- result{pkt, err}
	}()
	select {
	case r := <-ch:
		return r.pkt, remote, r.err
	case <-time.After(timeout):
		return nil, remote, fmt.Errorf("no packet after %s", timeout)
	}
}

// Close closes the peer connection.
func (p *Peer) Close() error { return p.PC.Close() }
