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
	"strconv"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/dashboard"
	"github.com/JoKeks2023/housephone/bridge/internal/fritzbox"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/lan"
	"github.com/JoKeks2023/housephone/bridge/internal/logsafe"
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
	pusher    calls.Pusher
	logRing   *admin.LogRing
	noAdmin   bool
	dashboard *dashboardOptions
}

type dashboardOptions struct {
	listen string
	gate   dashboard.Gate
}

// WithDashboard serves the web dashboard on its own listener behind gate.
// Only the Home Assistant add-on uses it (ingress); plain Docker stays with
// the shell and the TUI until HPHN-39 adds a passkey gate.
func WithDashboard(listen string, gate dashboard.Gate) Option {
	return func(o *options) { o.dashboard = &dashboardOptions{listen: listen, gate: gate} }
}

// WithLogRing lets the admin API show the recent log lines of ring.
func WithLogRing(r *admin.LogRing) Option {
	return func(o *options) { o.logRing = r }
}

// WithoutAdminSocket skips the admin socket (tests running several
// bridges on one data directory).
func WithoutAdminSocket() Option {
	return func(o *options) { o.noAdmin = true }
}

// WithPusher replaces the APNs pusher.
func WithPusher(p calls.Pusher) Option {
	return func(o *options) { o.pusher = p }
}

// Bridge is a running bridge instance.
type Bridge struct {
	cfg      config.Config
	log      *slog.Logger
	Identity store.Identity
	// Key signs every answer; devices pin its fingerprint when pairing.
	Key       *hp2.Identity
	Devices   *store.Devices
	Pairing   *store.Pairing
	manager   *calls.Manager
	sip       *sipleg.Leg
	engine    *media.Engine
	signaling *signaling.Server
	listener  net.Listener
	// privateListener serves the home network (nil if disabled).
	privateListener net.Listener
	trustedNetworks []*net.IPNet
	excludedNets    []*net.IPNet
	lanURL          string
	// directory is nil unless fritzbox.username is configured.
	directory *fritzbox.Directory

	startedAt   time.Time
	registrarIP string
	publicIP    *media.PublicIPSource
	apnsLoaded  bool
	recorder    *admin.Recorder
	logRing     *admin.LogRing
	admin       *admin.Server
	noAdmin     bool
	dashboard   *dashboardOptions
	// dashboardListener is nil without a dashboard.
	dashboardListener net.Listener
}

// New prepares all components; nothing is served before Run. The context
// bounds background helpers such as public IP detection.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, opts ...Option) (*Bridge, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	logsafe.SetShowNumbers(cfg.Log.ShowNumbers)
	if cfg.Log.ShowNumbers {
		log.Warn("log.showNumbers is on: phone numbers and caller names are logged in full")
	}
	identity, err := store.LoadOrCreateIdentity(cfg.Bridge.DataDir)
	if err != nil {
		return nil, fmt.Errorf("bridge identity: %w", err)
	}
	key, err := LoadIdentityKey(cfg.Bridge.DataDir, true)
	if err != nil {
		return nil, err
	}
	log.Info("bridge identity", "fingerprint", key.Fingerprint())
	b := &Bridge{
		cfg:      cfg,
		log:      log,
		Identity: identity,
		Key:      key,
		Devices:  store.NewDevices(cfg.Bridge.DataDir),
		Pairing:  store.NewPairing(cfg.Bridge.DataDir),

		startedAt: time.Now(),
		recorder:  admin.NewRecorder(50, nil),
		logRing:   o.logRing,
		noAdmin:   o.noAdmin,
		dashboard: o.dashboard,
	}

	// Credentials only go to the FRITZ!Box in the home network: resolve its
	// name once, require a local address and use that address from now on,
	// so a public "fritz.box" or a later DNS change cannot redirect logins.
	registrarIP, err := lan.Resolve(ctx, cfg.SIP.Registrar)
	if err != nil {
		return nil, fmt.Errorf("sip.registrar: %w", err)
	}
	registrar := registrarIP.String()
	b.registrarIP = registrar
	if registrar != cfg.SIP.Registrar {
		log.Info("FRITZ!Box address fixed", "registrar", cfg.SIP.Registrar, "ip", registrar)
	}
	trustedProxies, err := cfg.Bridge.TrustedProxyNets()
	if err != nil {
		return nil, err
	}
	b.trustedNetworks, err = cfg.Bridge.TrustedNetworkNets()
	if err != nil {
		return nil, err
	}
	b.excludedNets, err = cfg.Bridge.ExcludedNetworkNets()
	if err != nil {
		return nil, err
	}
	fritzBoxHost := registrar
	if cfg.FritzBox.Enabled() && cfg.FritzBox.Host != "" && cfg.FritzBox.Host != cfg.SIP.Registrar {
		ip, err := lan.Resolve(ctx, cfg.FritzBox.Host)
		if err != nil {
			return nil, fmt.Errorf("fritzbox.host: %w", err)
		}
		fritzBoxHost = ip.String()
	}

	router := ""
	if cfg.Media.PublicIPFromRouter {
		router = registrar
	}
	publicIP := media.NewPublicIPSource(ctx, media.PublicIPConfig{
		Static: cfg.Media.PublicIP,
		Host:   cfg.Media.PublicHost,
		Router: router,
		STUN:   cfg.Media.STUN,
		Detect: cfg.Media.DetectPublicIP,
	}, log)
	b.publicIP = publicIP
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
		b.apnsLoaded = true
	}
	if pusher == nil {
		log.Warn("APNs is not configured: devices only ring while the app is open")
	}

	var callerNames func(string) string
	var directory signaling.Directory
	if cfg.FritzBox.Enabled() {
		loc, err := time.LoadLocation(cfg.FritzBox.Timezone)
		if err != nil {
			b.engine.Close()
			return nil, fmt.Errorf("fritzbox.timezone: %w", err)
		}
		b.directory = fritzbox.NewDirectory(fritzbox.DirectoryConfig{
			Source: fritzbox.NewClient(fritzbox.ClientConfig{
				Host:      fritzBoxHost,
				PlainPort: strconv.Itoa(cfg.FritzBox.Port),
				Username:  cfg.FritzBox.Username,
				Password:  cfg.FritzBox.Password,
				Logger:    log,
			}),
			Location:    loc,
			CountryCode: cfg.FritzBox.CountryCode,
			Logger:      log,
		})
		callerNames = b.directory.CallerName
		directory = b.directory
	}

	iceServers := []protocol.ICEServer{}
	if len(cfg.Media.STUN) > 0 {
		iceServers = append(iceServers, protocol.ICEServer{URLs: cfg.Media.STUN})
	}
	b.manager = calls.NewManager(calls.Options{
		BridgeID:    identity.ID,
		ICEServers:  iceServers,
		Peers:       b.engine,
		Pusher:      pusher,
		Devices:     b.Devices,
		Logger:      log,
		CallerNames: callerNames,
		MaxCalls:    cfg.Bridge.MaxCalls,
		OnEvent:     b.recorder.Handle,
	})
	b.sip, err = sipleg.New(sipleg.Config{
		Registrar:      registrar,
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
	if cfg.Bridge.PrivateListen != "" {
		b.privateListener, err = net.Listen("tcp", cfg.Bridge.PrivateListen)
		if err != nil {
			b.engine.Close()
			return nil, fmt.Errorf("listen %s (bridge.privateListen): %w", cfg.Bridge.PrivateListen, err)
		}
		// The bound port (the configured one may be 0).
		bound := cfg
		host, _, _ := net.SplitHostPort(cfg.Bridge.PrivateListen)
		_, port, _ := net.SplitHostPort(b.privateListener.Addr().String())
		bound.Bridge.PrivateListen = net.JoinHostPort(host, port)
		b.lanURL = LanURL(bound, b.sip.BindHost())
	} else {
		log.Warn("bridge.privateListen is empty: devices cannot pair")
	}
	b.signaling = signaling.New(signaling.Config{
		BridgeID:          identity.ID,
		BridgeName:        cfg.Bridge.Name,
		BridgeVersion:     version.Version,
		Identity:          key,
		PublicURL:         cfg.Bridge.PublicURL,
		LanURL:            b.lanURL,
		PushTopic:         cfg.APNs.Topic,
		TrustProxyHeaders: cfg.Bridge.TrustProxyHeaders,
		TrustedProxies:    trustedProxies,
		Devices:           b.Devices,
		Pairing:           b.Pairing,
		Hub:               b.manager,
		Directory:         directory,
		Logger:            log,
	})

	b.listener, err = net.Listen("tcp", cfg.Bridge.Listen)
	if err != nil {
		if b.privateListener != nil {
			b.privateListener.Close()
		}
		b.engine.Close()
		return nil, fmt.Errorf("listen %s: %w", cfg.Bridge.Listen, err)
	}
	if o.dashboard != nil {
		// Like the admin socket a convenience: calls keep working without it.
		if l, err := net.Listen("tcp", o.dashboard.listen); err != nil {
			log.Error("dashboard unavailable", "listen", o.dashboard.listen, "error", err)
		} else {
			b.dashboardListener = l
		}
	}
	return b, nil
}

// DashboardAddr is the dashboard's listen address, empty if off.
func (b *Bridge) DashboardAddr() string {
	if b.dashboardListener == nil {
		return ""
	}
	return b.dashboardListener.Addr().String()
}

// Addr is the public HTTP listen address.
func (b *Bridge) Addr() string { return b.listener.Addr().String() }

// PrivateAddr is the private (home network) listen address, empty if
// disabled.
func (b *Bridge) PrivateAddr() string {
	if b.privateListener == nil {
		return ""
	}
	return b.privateListener.Addr().String()
}

// LanURL is the private listener URL sent to devices.
func (b *Bridge) LanURL() string { return b.lanURL }

// SIPRegistered reports the FRITZ!Box registration.
func (b *Bridge) SIPRegistered() bool { return b.sip.Registered() }

// Run serves until ctx ends, then shuts down gracefully.
func (b *Bridge) Run(ctx context.Context) error {
	newServer := func(h http.Handler) *http.Server {
		return &http.Server{
			Handler:           h,
			ReadHeaderTimeout: 10 * time.Second,
			// Keep-alive connections without a request are closed; WebSocket
			// connections are hijacked and not affected.
			IdleTimeout:    60 * time.Second,
			MaxHeaderBytes: 16 << 10,
		}
	}
	srv := newServer(b.signaling.PublicHandler())
	var privateSrv *http.Server
	errCh := make(chan error, 4)
	if !b.noAdmin {
		// The admin API is a convenience: without it the bridge still
		// serves calls, so a problem is logged instead of stopping it.
		adminSrv, err := admin.Listen(b.cfg.Bridge.DataDir, adminService{b: b}, b.log)
		if err != nil {
			b.log.Warn("admin socket unavailable: TUI and live device removal do not work", "error", err)
		} else {
			b.admin = adminSrv
			go func() {
				if err := adminSrv.Serve(); err != nil {
					b.log.Warn("admin socket stopped", "error", err)
				}
			}()
		}
	}
	var dashSrv *http.Server
	if b.dashboardListener != nil {
		dashSrv = newServer(dashboard.New(adminService{b: b}, b.dashboard.gate, b.log).Handler())
		go func() {
			if err := dashSrv.Serve(b.dashboardListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				b.log.Warn("dashboard stopped", "error", err)
			}
		}()
		b.log.Info("dashboard running", "listen", b.DashboardAddr())
	}
	go func() {
		if err := srv.Serve(b.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()
	if b.privateListener != nil {
		privateSrv = newServer(b.signaling.PrivateHandler(b.trustedNetworks, b.excludedNets...))
		go func() {
			if err := privateSrv.Serve(b.privateListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("http (private): %w", err)
			}
		}()
	}
	sipCtx, stopSIP := context.WithCancel(context.Background())
	sipDone := make(chan struct{})
	go func() {
		defer close(sipDone)
		if err := b.sip.Serve(sipCtx, b.manager.HandleIncoming); err != nil {
			errCh <- err
		}
	}()
	if b.directory != nil {
		go b.directory.Warmup(ctx)
	} else {
		b.log.Info("FRITZ!Box phonebook and call list not configured (fritzbox.username)")
	}
	b.log.Info("bridge running", "version", version.Version, "listen", b.Addr(), "privateListen", b.PrivateAddr(),
		"bridgeId", b.Identity.ID, "publicUrl", b.cfg.Bridge.PublicURL, "lanUrl", b.lanURL)

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
	if b.admin != nil {
		b.admin.Close(shutdownCtx)
	}
	_ = srv.Shutdown(shutdownCtx)
	if dashSrv != nil {
		_ = dashSrv.Shutdown(shutdownCtx)
	}
	if privateSrv != nil {
		_ = privateSrv.Shutdown(shutdownCtx)
	}
	stopSIP()
	select {
	case <-sipDone:
	case <-shutdownCtx.Done():
	}
	_ = b.engine.Close()
	return runErr
}

// PairingLink builds the housephone://pair link (v2) for a code; the
// fingerprint lets the device check it pairs with this bridge, and the
// device pairs over lanURL (the private listener).
func PairingLink(publicURL, lanURL, code, fingerprint, bridgeName string) string {
	return hp2.FormatPairingLink(publicURL, lanURL, code, fingerprint, bridgeName)
}

// LoadIdentityKey loads the bridge's Ed25519 key from the data directory,
// creating it (0600) if create is set and it does not exist yet.
func LoadIdentityKey(dataDir string, create bool) (*hp2.Identity, error) {
	var seed []byte
	var err error
	if create {
		seed, err = store.LoadOrCreateIdentityKey(dataDir, hp2.NewIdentitySeed)
	} else {
		seed, err = store.LoadIdentityKey(dataDir)
	}
	if err != nil {
		return nil, fmt.Errorf("bridge identity key %s: %w", store.IdentityKeyPath(dataDir), err)
	}
	key, err := hp2.NewIdentity(seed)
	if err != nil {
		return nil, fmt.Errorf("bridge identity key %s: %w", store.IdentityKeyPath(dataDir), err)
	}
	return key, nil
}
