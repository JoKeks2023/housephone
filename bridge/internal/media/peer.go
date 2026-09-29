package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
)

// ErrPeerClosed is returned by ReadRTP/WriteRTP after Close.
var ErrPeerClosed = errors.New("peer closed")

// peer is a WebRTC connection to one device (bridge = offerer).
type peer struct {
	pc      *webrtc.PeerConnection
	sender  *webrtc.RTPSender
	offered []codec.Codec
	log     *slog.Logger

	mu     sync.Mutex
	track  *webrtc.TrackLocalStaticRTP
	chosen codec.Codec

	remote    chan *webrtc.TrackRemote
	closed    chan struct{}
	closeOnce sync.Once
}

func newPeer(api *webrtc.API, codecs []codec.Codec, onState func(calls.PeerState), log *slog.Logger) (*peer, error) {
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}
	p := &peer{
		pc:      pc,
		offered: codecs,
		log:     log,
		chosen:  codecs[0],
		remote:  make(chan *webrtc.TrackRemote, 1),
		closed:  make(chan struct{}),
	}
	track, err := webrtc.NewTrackLocalStaticRTP(codecCapability(codecs[0]), "audio", "housephone")
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	p.track = track
	sender, err := pc.AddTrack(track)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("add track: %w", err)
	}
	p.sender = sender
	// Drain RTCP so interceptors (receiver reports) keep working.
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if remote.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		select {
		case p.remote <- remote:
		default:
			// Renegotiation (ICE restart) keeps the track; ignore duplicates.
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if onState == nil {
			return
		}
		switch s {
		case webrtc.PeerConnectionStateConnected:
			onState(calls.PeerConnected)
		case webrtc.PeerConnectionStateDisconnected:
			onState(calls.PeerDisconnected)
		case webrtc.PeerConnectionStateFailed:
			onState(calls.PeerFailed)
		case webrtc.PeerConnectionStateClosed:
			onState(calls.PeerClosed)
		}
	})
	return p, nil
}

// CreateOffer implements calls.Peer.
func (p *peer) CreateOffer(ctx context.Context, iceRestart bool) (string, error) {
	var opts *webrtc.OfferOptions
	if iceRestart {
		opts = &webrtc.OfferOptions{ICERestart: true}
	}
	offer, err := p.pc.CreateOffer(opts)
	if err != nil {
		return "", fmt.Errorf("create offer: %w", err)
	}
	gathered := webrtc.GatheringCompletePromise(p.pc)
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("set local description: %w", err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return "", fmt.Errorf("ICE gathering: %w", ctx.Err())
	case <-p.closed:
		return "", ErrPeerClosed
	}
	local := p.pc.LocalDescription()
	if local == nil {
		return "", errors.New("no local description")
	}
	return local.SDP, nil
}

// SetAnswer implements calls.Peer. The device's first audio codec decides
// what the bridge sends; the local track is switched before the answer is
// applied so it binds to a negotiated codec.
func (p *peer) SetAnswer(answer string) error {
	chosen, err := firstAudioCodec(answer, p.offered)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if chosen != p.chosen {
		track, err := webrtc.NewTrackLocalStaticRTP(codecCapability(chosen), "audio", "housephone")
		if err != nil {
			p.mu.Unlock()
			return err
		}
		if err := p.sender.ReplaceTrack(track); err != nil {
			p.mu.Unlock()
			return fmt.Errorf("switch to %s: %w", chosen, err)
		}
		p.track, p.chosen = track, chosen
	}
	p.mu.Unlock()
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		return fmt.Errorf("set remote description: %w", err)
	}
	return nil
}

// Codec implements calls.Peer.
func (p *peer) Codec() (codec.Codec, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chosen, nil
}

// ReadRTP implements calls.Peer.
func (p *peer) ReadRTP() (*rtp.Packet, error) {
	var remote *webrtc.TrackRemote
	select {
	case remote = <-p.remote:
		// Put it back for the next call.
		p.remote <- remote
	case <-p.closed:
		return nil, ErrPeerClosed
	}
	pkt, _, err := remote.ReadRTP()
	if err != nil {
		return nil, err
	}
	return pkt, nil
}

// WriteRTP implements calls.Peer.
func (p *peer) WriteRTP(pkt *rtp.Packet) error {
	select {
	case <-p.closed:
		return ErrPeerClosed
	default:
	}
	p.mu.Lock()
	track := p.track
	p.mu.Unlock()
	return track.WriteRTP(pkt)
}

// Close implements calls.Peer.
func (p *peer) Close() error {
	var err error
	p.closeOnce.Do(func() {
		close(p.closed)
		err = p.pc.Close()
	})
	return err
}

// firstAudioCodec returns the first codec of the answer's audio section that
// was offered.
func firstAudioCodec(answer string, offered []codec.Codec) (codec.Codec, error) {
	var desc sdp.SessionDescription
	if err := desc.UnmarshalString(answer); err != nil {
		return "", fmt.Errorf("parse answer: %w", err)
	}
	for _, md := range desc.MediaDescriptions {
		if md.MediaName.Media != "audio" {
			continue
		}
		if md.MediaName.Port.Value == 0 {
			return "", errors.New("audio rejected in answer")
		}
		names := rtpmapNames(md)
		for _, format := range md.MediaName.Formats {
			pt, err := strconv.Atoi(format)
			if err != nil {
				continue
			}
			name, ok := names[pt]
			if !ok {
				name = staticName(pt)
			}
			if c, ok := codec.FromName(name); ok && containsCodec(offered, c) {
				return c, nil
			}
		}
		return "", errors.New("answer contains no offered audio codec")
	}
	return "", errors.New("answer has no audio section")
}

func rtpmapNames(md *sdp.MediaDescription) map[int]string {
	names := map[int]string{}
	for _, a := range md.Attributes {
		if a.Key != "rtpmap" {
			continue
		}
		ptStr, rest, ok := strings.Cut(a.Value, " ")
		if !ok {
			continue
		}
		pt, err := strconv.Atoi(ptStr)
		if err != nil {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		names[pt] = name
	}
	return names
}

func staticName(pt int) string {
	switch pt {
	case 0:
		return "PCMU"
	case 8:
		return "PCMA"
	case 9:
		return "G722"
	}
	return ""
}

func containsCodec(list []codec.Codec, c codec.Codec) bool {
	for _, x := range list {
		if x == c {
			return true
		}
	}
	return false
}
