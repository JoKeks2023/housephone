// Package app wires the bridge together: SIP leg, call manager, WebRTC
// engine, APNs pusher and the signaling server.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/media"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/push"
	"github.com/JoKeks2023/housephone/bridge/internal/signaling"
	"github.com/JoKeks2023/housephone/bridge/internal/sipleg"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
	"github.com/JoKeks2023/housephone/bridge/internal/version"
)

// Option customizes a Bridge (tests).
type Option func(*options)

type options struct {
	pusher calls.Pusher
}

// WithPusher replaces the APNs pusher.
func WithPusher(p calls.Pusher) Option {
	return func(o *options) { o.pusher = p }
}

// Bridge is a running bridge instance.
type Bridge struct {
	cfg       config.Config
	log       *slog.Logger
	Identity  store.Identity
	Devices   *store.Devices
	Pairing   *store.Pairing
	manager   *calls.Manager
	sip       *sipleg.Leg
	engine    *media.Engine
	signaling *signaling.Server
	listener  net.Listener
}

// New prepares all components; nothing is served before Run. The context
// bounds background helpers such as public IP detection.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, opts ...Option) (*Bridge, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	identity, err := store.LoadOrCreateIdentity(cfg.Bridge.DataDir)
	if err != nil {
		return nil, fmt.Errorf("bridge identity: %w", err)
	}
	b := &Bridge{
		cfg:      cfg,
		log:      log,
		Identity: identity,
		Devices:  store.NewDevices(cfg.Bridge.DataDir),
		Pairing:  store.NewPairing(cfg.Bridge.DataDir),
	}

	router := ""
	if cfg.Media.PublicIPFromRouter {
		router = cfg.SIP.Registrar
	}
	publicIP := media.NewPublicIPSource(ctx, media.PublicIPConfig{
		Static: cfg.Media.PublicIP,
		Host:   cfg.Media.PublicHost,
		Router: router,
		STUN:   cfg.Media.STUN,
		Detect: cfg.Media.DetectPublicIP,
	}, log)
	b.engine, err = media.NewEngine(media.EngineConfig{
		UDPPort:         cfg.Media.UDPPort,
		PublicIP:        publicIP.Get,
		Interfaces:      cfg.Media.Interfaces,
		IncludeLoopback: cfg.Media.IncludeLoopback,
		Logger:          log,
	})
	if err != nil {
		return nil, err
	}

	pusher := o.pusher
	if pusher == nil && cfg.APNs.Enabled() {
		apns, err := push.NewAPNs(push.Config{KeyFile: cfg.APNs.KeyFile, KeyID: cfg.APNs.KeyID, TeamID: cfg.APNs.TeamID, Topic: cfg.APNs.Topic})
		if err != nil {
			b.engine.Close()
			return nil, err
		}
		pusher = apns
	}
	if pusher == nil {
		log.Warn("APNs is not configured: devices only ring while the app is open")
	}

	iceServers := []protocol.ICEServer{}
	if len(cfg.Media.STUN) > 0 {
		iceServers = append(iceServers, protocol.ICEServer{URLs: cfg.Media.STUN})
	}
	b.manager = calls.NewManager(calls.Options{
		BridgeID:   identity.ID,
		ICEServers: iceServers,
		Peers:      b.engine,
		Pusher:     pusher,
		Devices:    b.Devices,
		Logger:     log,
	})
	b.sip, err = sipleg.New(sipleg.Config{
		Registrar:      cfg.SIP.Registrar,
		Port:           cfg.SIP.Port,
		Username:       cfg.SIP.Username,
		Password:       cfg.SIP.Password,
		BindHost:       cfg.SIP.BindHost,
		BindPort:       cfg.SIP.BindPort,
		RTPPortMin:     cfg.SIP.RTPPortMin,
		RTPPortMax:     cfg.SIP.RTPPortMax,
		RegisterExpiry: time.Duration(cfg.SIP.RegisterExpirySeconds) * time.Second,
		OnRegistration: b.manager.BroadcastStatus,
		Logger:         log,
	})
	if err != nil {
		b.engine.Close()
		return nil, err
	}
	b.manager.SetSIP(b.sip)
	b.signaling = signaling.New(signaling.Config{
		BridgeID:          identity.ID,
		BridgeName:        cfg.Bridge.Name,
		BridgeVersion:     version.Version,
		PublicURL:         cfg.Bridge.PublicURL,
		PushTopic:         cfg.APNs.Topic,
		TrustProxyHeaders: cfg.Bridge.TrustProxyHeaders,
		Devices:           b.Devices,
		Pairing:           b.Pairing,
		Hub:               b.manager,
		Logger:            log,
	})

	b.listener, err = net.Listen("tcp", cfg.Bridge.Listen)
	if err != nil {
		b.engine.Close()
		return nil, fmt.Errorf("listen %s: %w", cfg.Bridge.Listen, err)
	}
	return b, nil
}

// Addr is the HTTP listen address.
func (b *Bridge) Addr() string { return b.listener.Addr().String() }

// SIPRegistered reports the FRITZ!Box registration.
func (b *Bridge) SIPRegistered() bool { return b.sip.Registered() }

// Run serves until ctx ends, then shuts down gracefully.
func (b *Bridge) Run(ctx context.Context) error {
	srv := &http.Server{
		Handler:           b.signaling.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 2)
	go func() {
		if err := srv.Serve(b.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()
	sipCtx, stopSIP := context.WithCancel(context.Background())
	sipDone := make(chan struct{})
	go func() {
		defer close(sipDone)
		if err := b.sip.Serve(sipCtx, b.manager.HandleIncoming); err != nil {
			errCh <- err
		}
	}()
	b.log.Info("bridge running", "version", version.Version, "listen", b.Addr(), "bridgeId", b.Identity.ID, "publicUrl", b.cfg.Bridge.PublicURL)

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errCh:
	}

	b.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b.manager.Shutdown(shutdownCtx)
	b.signaling.Shutdown(shutdownCtx)
	_ = srv.Shutdown(shutdownCtx)
	stopSIP()
	select {
	case <-sipDone:
	case <-shutdownCtx.Done():
	}
	_ = b.engine.Close()
	return runErr
}

// PairingLink builds the housephone://pair link for a code.
func PairingLink(publicURL, code, bridgeName string) string {
	q := url.Values{}
	q.Set("url", publicURL)
	q.Set("code", code)
	q.Set("name", bridgeName)
	// url.Values.Encode uses "+" for spaces; the spec expects percent-encoding.
	return "housephone://pair?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}
