// Package media creates the WebRTC peers towards devices. All peers share a
// single UDP port (ICE UDP mux), so only one port has to be forwarded on the
// FRITZ!Box.
package media

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
)

// EngineConfig configures the shared WebRTC engine.
type EngineConfig struct {
	// UDPPort is the single media port (all interfaces).
	UDPPort int
	// PublicIP returns the current public IPv4 address, or "" if unknown.
	// It is advertised as an additional server-reflexive candidate.
	PublicIP func() string
	// Interfaces restricts host candidates to these interface names.
	Interfaces []string
	// IncludeLoopback adds loopback candidates (tests only).
	IncludeLoopback bool
	Logger          *slog.Logger
}

// Engine creates device peers sharing one UDP port.
type Engine struct {
	cfg EngineConfig
	mux *ice.MultiUDPMuxDefault
	log *slog.Logger
}

// NewEngine opens the UDP port on all selected interfaces.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.PublicIP == nil {
		cfg.PublicIP = func() string { return "" }
	}
	opts := []ice.UDPMuxFromPortOption{
		ice.UDPMuxFromPortWithInterfaceFilter(interfaceFilter(cfg.Interfaces)),
		ice.UDPMuxFromPortWithIPFilter(ipFilter),
	}
	if cfg.IncludeLoopback {
		opts = append(opts, ice.UDPMuxFromPortWithLoopback())
	}
	mux, err := ice.NewMultiUDPMuxFromPort(cfg.UDPPort, opts...)
	if err != nil {
		return nil, fmt.Errorf("open media UDP port %d: %w", cfg.UDPPort, err)
	}
	e := &Engine{cfg: cfg, mux: mux, log: cfg.Logger.With("component", "media")}
	addrs := make([]string, 0)
	for _, a := range mux.GetListenAddresses() {
		addrs = append(addrs, a.String())
	}
	e.log.Info("media port open", "addresses", addrs)
	return e, nil
}

// Close releases the UDP port.
func (e *Engine) Close() error {
	return e.mux.Close()
}

// interfaceFilter keeps the configured interfaces, or everything except
// container bridges and virtual Ethernet pairs.
func interfaceFilter(allow []string) func(string) bool {
	return func(name string) bool {
		if len(allow) > 0 {
			return slices.Contains(allow, name)
		}
		for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "tailscale", "utun", "wg"} {
			if strings.HasPrefix(name, prefix) {
				return false
			}
		}
		return true
	}
}

// ipFilter drops IPv6 link-local and unique-local addresses, which no phone
// outside the LAN can reach.
func ipFilter(ip net.IP) bool {
	if ip.To4() != nil {
		return !ip.IsLinkLocalUnicast()
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate()
}

// NewPeer implements calls.PeerFactory.
func (e *Engine) NewPeer(codecs []codec.Codec, onState func(calls.PeerState)) (calls.Peer, error) {
	if len(codecs) == 0 {
		return nil, errors.New("at least one codec is required")
	}
	me := &webrtc.MediaEngine{}
	for _, c := range codecs {
		if err := me.RegisterCodec(codecParameters(c), webrtc.RTPCodecTypeAudio); err != nil {
			return nil, fmt.Errorf("register %s: %w", c, err)
		}
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(me, registry); err != nil {
		return nil, fmt.Errorf("register interceptors: %w", err)
	}

	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(e.mux)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	se.SetIncludeLoopbackCandidate(e.cfg.IncludeLoopback)
	se.SetICETimeouts(5*time.Second, 20*time.Second, 2*time.Second)
	if ip := e.cfg.PublicIP(); ip != "" {
		err := se.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External:        []string{ip},
			AsCandidateType: webrtc.ICECandidateTypeSrflx,
			Mode:            webrtc.ICEAddressRewriteAppend,
			Networks:        []webrtc.NetworkType{webrtc.NetworkTypeUDP4},
		})
		if err != nil {
			return nil, fmt.Errorf("public IP candidate: %w", err)
		}
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se), webrtc.WithInterceptorRegistry(registry))
	return newPeer(api, codecs, onState, e.log)
}

func codecParameters(c codec.Codec) webrtc.RTPCodecParameters {
	return webrtc.RTPCodecParameters{
		RTPCodecCapability: codecCapability(c),
		PayloadType:        webrtc.PayloadType(c.PayloadType()),
	}
}

func codecCapability(c codec.Codec) webrtc.RTPCodecCapability {
	return webrtc.RTPCodecCapability{MimeType: c.MimeType(), ClockRate: codec.ClockRate}
}
