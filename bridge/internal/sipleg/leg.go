// Package sipleg registers the bridge at the FRITZ!Box as an IP phone and
// adapts SIP dialogs (via diago/sipgo) to the calls package interfaces.
package sipleg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
)

// Config configures the SIP leg.
type Config struct {
	// Registrar is the FRITZ!Box host ("fritz.box" or its IP).
	Registrar string
	Port      int
	Username  string
	Password  string
	// BindHost is the local IP. Empty: the IP that routes to the registrar.
	BindHost   string
	BindPort   int
	RTPPortMin int
	RTPPortMax int
	// RegisterExpiry is the requested registration lifetime.
	RegisterExpiry time.Duration
	// AllowAnySource accepts INVITEs from any address (tests only). By
	// default only the registrar may send INVITEs.
	AllowAnySource bool
	// OnRegistration is called when the registration state changes.
	OnRegistration func(registered bool)
	Logger         *slog.Logger
}

// Leg is the SIP connection to the FRITZ!Box.
type Leg struct {
	cfg        Config
	log        *slog.Logger
	ua         *sipgo.UserAgent
	dg         *diago.Diago
	bindHost   string
	registered atomic.Bool

	allowedMu sync.RWMutex
	allowed   map[string]bool

	cancels sync.Map // Call-ID → CANCEL *sip.Request
}

// New prepares the SIP user agent. Call Serve to start.
func New(cfg Config) (*Leg, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Port == 0 {
		cfg.Port = 5060
	}
	if cfg.RegisterExpiry == 0 {
		cfg.RegisterExpiry = 5 * time.Minute
	}
	log := cfg.Logger.With("component", "sip")

	bindHost := cfg.BindHost
	if bindHost == "" {
		ip, err := routeIP(cfg.Registrar, cfg.Port)
		if err != nil {
			return nil, fmt.Errorf("find local IP towards %s: %w (set sip.bindHost)", cfg.Registrar, err)
		}
		bindHost = ip
	}
	if cfg.RTPPortMin > 0 && cfg.RTPPortMax > cfg.RTPPortMin {
		media.RTPPortStart = cfg.RTPPortMin
		media.RTPPortEnd = cfg.RTPPortMax
	}

	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent(cfg.Username),
		sipgo.WithUserAgentHostname(cfg.Registrar),
	)
	if err != nil {
		return nil, err
	}
	l := &Leg{cfg: cfg, log: log, ua: ua, bindHost: bindHost, allowed: map[string]bool{}}

	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, err
	}
	// The FRITZ!Box checks registered phones with OPTIONS.
	srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil))
	})

	l.dg = diago.NewDiago(ua,
		diago.WithServer(srv),
		diago.WithLogger(log),
		diago.WithTransport(diago.Transport{Transport: "udp", BindHost: bindHost, BindPort: cfg.BindPort}),
		diago.WithMediaConfig(diago.MediaConfig{Codecs: localCodecs}),
		diago.WithServerRequestMiddleware(l.middleware),
	)
	l.refreshAllowed()
	log.Info("SIP leg ready", "bind", net.JoinHostPort(bindHost, strconv.Itoa(cfg.BindPort)), "registrar", cfg.Registrar, "user", cfg.Username)
	return l, nil
}

// routeIP returns the local IP used to reach host.
func routeIP(host string, port int) (string, error) {
	conn, err := net.Dial("udp4", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// BindHost returns the local SIP/RTP address.
func (l *Leg) BindHost() string { return l.bindHost }

// Registered implements calls.SIPLeg.
func (l *Leg) Registered() bool { return l.registered.Load() }

func (l *Leg) setRegistered(v bool) {
	if l.registered.Swap(v) != v {
		if v {
			l.log.Info("registered at FRITZ!Box")
		} else {
			l.log.Warn("not registered at FRITZ!Box")
		}
		if l.cfg.OnRegistration != nil {
			l.cfg.OnRegistration(v)
		}
	}
}

// refreshAllowed resolves the registrar; only it may send INVITEs.
func (l *Leg) refreshAllowed() {
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), l.cfg.Registrar)
	if err != nil {
		l.log.Warn("resolving registrar failed", "registrar", l.cfg.Registrar, "error", err)
		return
	}
	allowed := map[string]bool{}
	for _, ip := range ips {
		allowed[ip.IP.String()] = true
	}
	l.allowedMu.Lock()
	l.allowed = allowed
	l.allowedMu.Unlock()
}

func (l *Leg) sourceAllowed(source string) bool {
	if l.cfg.AllowAnySource {
		return true
	}
	host, _, err := net.SplitHostPort(source)
	if err != nil {
		host = source
	}
	l.allowedMu.RLock()
	defer l.allowedMu.RUnlock()
	return l.allowed[host]
}

// middleware rejects INVITEs not coming from the FRITZ!Box and remembers
// CANCEL requests so their Reason header can be inspected.
func (l *Leg) middleware(next sipgo.RequestHandler) sipgo.RequestHandler {
	return func(req *sip.Request, tx sip.ServerTransaction) {
		if req.IsInvite() {
			if !l.sourceAllowed(req.Source()) {
				l.log.Warn("rejecting INVITE from unexpected source", "source", req.Source())
				_ = tx.Respond(sip.NewResponseFromRequest(req, sip.StatusForbidden, "Forbidden", nil))
				return
			}
			if callID := req.CallID(); callID != nil {
				id := callID.Value()
				tx.OnCancel(func(cancel *sip.Request) {
					l.cancels.Store(id, cancel)
				})
			}
		}
		next(req, tx)
	}
}

// Serve registers and handles incoming calls until ctx ends.
func (l *Leg) Serve(ctx context.Context, handler func(context.Context, calls.IncomingSIPCall)) error {
	if err := l.dg.ServeBackground(ctx, func(d *diago.DialogServerSession) {
		l.handleInvite(ctx, d, handler)
	}); err != nil {
		return fmt.Errorf("listen SIP: %w", err)
	}
	l.registerLoop(ctx)
	return nil
}

func (l *Leg) registerLoop(ctx context.Context) {
	backoff := 5 * time.Second
	for {
		err := l.registerOnce(ctx)
		l.setRegistered(false)
		if ctx.Err() != nil {
			return
		}
		l.log.Warn("registration failed, retrying", "error", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
		l.refreshAllowed()
	}
}

// registerOnce registers and keeps the registration fresh until it fails.
func (l *Leg) registerOnce(ctx context.Context) error {
	aor := sip.Uri{Scheme: "sip", User: l.cfg.Username, Host: l.cfg.Registrar}
	recipient := sip.Uri{Scheme: "sip", Host: l.cfg.Registrar, Port: l.cfg.Port}
	tx, err := l.dg.RegisterTransaction(ctx, recipient, diago.RegisterOptions{
		Username: l.cfg.Username,
		Password: l.cfg.Password,
		Expiry:   l.cfg.RegisterExpiry,
	})
	if err != nil {
		return err
	}
	tx.Origin.AppendHeader(&sip.FromHeader{Address: aor, Params: sip.NewParams()})
	tx.Origin.AppendHeader(&sip.ToHeader{Address: aor, Params: sip.NewParams()})
	if err := tx.Register(ctx); err != nil {
		return err
	}
	l.setRegistered(true)
	defer func() {
		// Best effort unregister on shutdown.
		if ctx.Err() != nil {
			unregCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = tx.Unregister(unregCtx)
		}
	}()
	return tx.QualifyLoop(ctx)
}

// endReason determines why an incoming dialog ended.
func (l *Leg) endReason(d *diago.DialogServerSession, answered bool) calls.SIPEndReason {
	cause := context.Cause(d.Context())
	if errors.Is(cause, sip.ErrTransactionCanceled) {
		if v, ok := l.cancels.Load(d.InviteRequest.CallID().Value()); ok && answeredElsewhere(v.(*sip.Request)) {
			return calls.SIPEndAnsweredElsewhere
		}
		return calls.SIPEndCancelled
	}
	if answered {
		return calls.SIPEndRemoteHangup
	}
	return calls.SIPEndFailed
}

// answeredElsewhere detects "Reason: SIP;cause=200" (RFC 3326) on CANCEL.
func answeredElsewhere(cancel *sip.Request) bool {
	for _, h := range cancel.GetHeaders("Reason") {
		v := strings.ToLower(strings.ReplaceAll(h.Value(), " ", ""))
		if strings.HasPrefix(v, "sip;") && strings.Contains(v, "cause=200") {
			return true
		}
	}
	return false
}
