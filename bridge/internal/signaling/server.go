// Package signaling serves the device endpoints of the signaling protocol:
// the sealed WebSocket (/v1/ws), pairing (/v1/pair), the HTTPS device
// endpoints (/v1/device, /v1/calls/{id}, /v1/phonebook, /v1/history) and
// the health check. Authentication and sealing follow signaling v2
// (ADR-0004, package hp2).
package signaling

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/profile"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/pushseal"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Hub receives authenticated device traffic (implemented by calls.Manager).
type Hub interface {
	DeviceConnected(calls.DeviceConn)
	DeviceDisconnected(calls.DeviceConn)
	HandleDeviceMessage(calls.DeviceConn, protocol.Envelope)
	SIPRegistered() bool
	// SIPRegisteredFor is the registration of one profile's line
	// (ADR-0008).
	SIPRegisteredFor(profileID string) bool
	// CallStatus answers GET /v1/calls/{callId} for one device (v1.1).
	CallStatus(deviceID, callID string) (protocol.CallStatus, bool)
	// DeviceRevoked ends the calls of a device that was removed while it
	// was connected.
	DeviceRevoked(calls.DeviceConn)
}

// Directory serves the FRITZ!Box phonebook and call list (v1.2,
// implemented by fritzbox.Directory). Errors may implement
// UserMessage() string for the error message sent to the app.
type Directory interface {
	// Phonebook returns the contacts of the given FRITZ!Box phonebooks
	// (nil: all).
	Phonebook(ctx context.Context, books []string) (body []byte, etag string, err error)
	// History returns the call list; with own numbers only the calls on
	// them (nil: all).
	History(ctx context.Context, limit int, own []string) ([]byte, error)
	Features() []string
}

// Config configures the server.
type Config struct {
	BridgeID      string
	BridgeName    string
	BridgeVersion string
	// Identity signs pairing answers and every answer to a device (v2).
	Identity *hp2.Identity
	// PublicURL is the wss:// URL sent in pair.companion (v1.1).
	PublicURL string
	// LanURL is the ws:// URL of the private listener, sent in welcome and
	// pair.companion. Empty without a private listener.
	LanURL string
	// PushTopic is the configured APNs topic; per-device topics must share
	// its bundle prefix (v1.1).
	PushTopic string
	// TrustProxyHeaders uses CF-Connecting-IP / X-Forwarded-For, but only
	// from requests that come from loopback (cloudflared, a local proxy) or
	// from TrustedProxies. Anyone else could set the headers freely.
	TrustProxyHeaders bool
	TrustedProxies    []*net.IPNet
	Devices           *store.Devices
	Pairing           *store.Pairing
	Hub               Hub
	// Directory is nil when fritzbox.username is not configured.
	Directory Directory
	// Profiles are the household profiles (ADR-0008); the zero value is a
	// single default profile.
	Profiles profile.Set
	Logger   *slog.Logger

	PingInterval     time.Duration
	FirstMessageWait time.Duration
	// RevalidateInterval is how often an open connection checks that its
	// device is still paired (devices remove takes effect within it).
	RevalidateInterval time.Duration
	// LanPairTTL is how long a LAN pairing request waits for the admin
	// (default DefaultLanPairTTL).
	LanPairTTL time.Duration
	// Admin runs admin actions from the app (ADR-0009); nil disables
	// /v1/admin/*.
	Admin AdminBackend
	Now   func() time.Time
}

// Limits.
const (
	maxMessageBytes    = 64 << 10
	sendQueue          = 64
	writeTimeout       = 10 * time.Second
	closeGrace         = time.Second
	maxDeviceNameRunes = 64
	maxPushTokenLength = 200
	// companionCodesPerHour bounds pair.companion.request per device.
	companionCodesPerHour = 5
	// httpReadTimeout bounds reading a request body of the plain HTTP
	// endpoints (bodies are at most maxHTTPBody).
	httpReadTimeout = 15 * time.Second
	// httpWriteTimeout bounds answering; a phonebook fetch from the
	// FRITZ!Box takes several TR-064 requests.
	httpWriteTimeout = 60 * time.Second
)

// Server is the HTTP handler for /v1/*.
type Server struct {
	cfg     Config
	log     *slog.Logger
	limiter *rateLimiter
	// companions limits pair.companion.request per device ID.
	companions *rateLimiter
	// warnings throttles warnings about failed logins per client.
	warnings *logThrottle
	// nonces rejects replayed requests (v2).
	nonces *nonceCache
	// lan holds the pairing requests from the home network (ADR-0007).
	lan *lanPairing

	mu       sync.Mutex
	sessions map[string]*session
	active   sync.WaitGroup
}

// New creates a server. cfg.Identity is required.
func New(cfg Config) *Server {
	if cfg.Identity == nil {
		panic("signaling: Config.Identity is required")
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 20 * time.Second
	}
	if cfg.FirstMessageWait == 0 {
		cfg.FirstMessageWait = 10 * time.Second
	}
	if cfg.RevalidateInterval == 0 {
		cfg.RevalidateInterval = 10 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{
		cfg:        cfg,
		log:        cfg.Logger.With("component", "signaling"),
		limiter:    newRateLimiter(5, time.Minute, cfg.Now),
		companions: newRateLimiter(companionCodesPerHour, time.Hour, cfg.Now),
		warnings:   newLogThrottle(time.Minute, cfg.Now),
		nonces:     newNonceCache(),
		lan:        newLanPairing(cfg.Now),
		sessions:   map[string]*session{},
	}
}

// PublicHandler returns the routes of the public listener (behind the
// Cloudflare Tunnel): telephony for paired devices only. Pairing answers
// 404 and pair.companion.request an error.
func (s *Server) PublicHandler() http.Handler { return s.routes(false) }

// PrivateHandler returns the routes of the private listener: everything,
// including pairing, but only for clients whose address (RemoteAddr, never
// a proxy header) lies in trusted.
func (s *Server) PrivateHandler(trusted []*net.IPNet, excluded ...*net.IPNet) http.Handler {
	return s.sourceFilter(trusted, excluded, s.routes(true))
}

// Handler returns all routes without a source filter (tests).
func (s *Server) Handler() http.Handler { return s.routes(true) }

func (s *Server) routes(private bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("GET /v1/ws", func(w http.ResponseWriter, r *http.Request) { s.websocket(w, r, private) })
	if private {
		mux.HandleFunc("POST /v1/pair", withDeadline(s.httpPair))
		mux.HandleFunc("POST /v1/pair/lan", withDeadline(s.httpLanStart))
		mux.HandleFunc("POST /v1/pair/lan/{id}/reveal", withDeadline(s.httpLanReveal))
		mux.HandleFunc("GET /v1/pair/lan/{id}", withDeadline(s.httpLanState))
	} else {
		mux.HandleFunc("POST /v1/pair/lan", s.homeNetworkOnly)
		mux.HandleFunc("POST /v1/pair/lan/{id}/reveal", s.homeNetworkOnly)
		mux.HandleFunc("GET /v1/pair/lan/{id}", s.homeNetworkOnly)
	}
	mux.HandleFunc("PUT /v1/device", s.authed(s.httpUpdateDevice))
	mux.HandleFunc("DELETE /v1/device", s.authed(s.httpDeleteDevice))
	mux.HandleFunc("GET /v1/calls/{callId}", s.authed(s.httpCallStatus))
	mux.HandleFunc("GET /v1/phonebook", s.authed(s.httpPhonebook))
	mux.HandleFunc("GET /v1/history", s.authed(s.httpHistory))
	s.adminRoutes(mux, private)
	mux.HandleFunc("/", s.notFound)
	return mux
}

// sourceFilter lets only clients from trusted networks through. It looks at
// the TCP peer only: a proxy on the same host (cloudflared) connects from
// loopback, which is why loopback is not trusted unless listed explicitly.
// Sources in excluded are rejected even inside a trusted network.
func (s *Server) sourceFilter(trusted, excluded []*net.IPNet, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !TrustedSource(trusted, r.RemoteAddr) || (len(excluded) > 0 && TrustedSource(excluded, r.RemoteAddr)) {
			s.warnClient("private listener: untrusted source", peerHost(r.RemoteAddr))
			s.writeSignedError(w, s.sessionFromHeader(r), http.StatusForbidden, protocol.Error{Code: protocol.ErrorHomeNetworkRequired, Message: "only reachable from the home network"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TrustedSource reports whether the peer address (host:port or host) lies
// in one of the trusted networks.
func TrustedSource(trusted []*net.IPNet, remoteAddr string) bool {
	host := peerHost(remoteAddr)
	// Zone of a link-local address (fe80::1%en0).
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func peerHost(remoteAddr string) string {
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return h
	}
	return remoteAddr
}

// notFound answers unknown paths and methods with 404, signed when the
// request carries a well-formed HP2 header, so the app can trust even that.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.writeSignedError(w, s.sessionFromHeader(r), http.StatusNotFound, protocol.Error{Code: protocol.ErrorBadRequest, Message: "not found"})
}

// withDeadline bounds reading the body and writing the answer of a plain
// HTTP request, so slow clients cannot hold connections open. The WebSocket
// route is left out: its connection lives as long as the device is online.
func withDeadline(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		now := time.Now()
		_ = rc.SetReadDeadline(now.Add(httpReadTimeout))
		_ = rc.SetWriteDeadline(now.Add(httpWriteTimeout))
		h(w, r)
	}
}

// health answers {"status":"ok"} to anyone. A paired device that signs the
// request also gets the version and the FRITZ!Box registration (sealed).
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}
	s.authed(func(w http.ResponseWriter, _ *http.Request, _ store.Device) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        "ok",
			"version":       s.cfg.BridgeVersion,
			"sipRegistered": s.cfg.Hub.SIPRegistered(),
		})
	})(w, r)
}

// websocket upgrades an HP2-authenticated request to the sealed device
// connection. Without valid authentication there is no upgrade (401).
// private marks connections of the private listener; only they may request
// companion pairing codes.
func (s *Server) websocket(w http.ResponseWriter, r *http.Request, private bool) {
	s.active.Add(1)
	defer s.active.Done()
	res := s.checkRequest(r, nil)
	if res.code != "" {
		s.rejectAuth(w, r, res)
		return
	}
	// The 101 carries the bridge's signature; coder/websocket keeps
	// headers set before Accept.
	w.Header().Set(hp2.BridgeHeader, res.sess.SignAnswer(s.cfg.Identity, http.StatusSwitchingProtocols, nil))
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(maxMessageBytes + hp2.FrameOverhead + 1)
	conn, err := hp2.NewConn(ws, res.sess.Keys.BridgeToDevice, res.sess.Keys.DeviceToBridge)
	if err != nil {
		ws.CloseNow()
		return
	}
	s.runDevice(r.Context(), conn, res.dev, s.clientIP(r), private)
}

// clientIP is the address used for rate limits and logs. Proxy headers are
// only believed from a trusted proxy; from X-Forwarded-For the rightmost
// entry is used, the one the trusted proxy added itself.
func (s *Server) clientIP(r *http.Request) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remote = host
	}
	if !s.cfg.TrustProxyHeaders || !s.trustedProxy(remote) {
		return remote
	}
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); ip != nil {
		return ip.String()
	}
	if fwd := r.Header.Values("X-Forwarded-For"); len(fwd) > 0 {
		entries := strings.Split(fwd[len(fwd)-1], ",")
		if ip := net.ParseIP(strings.TrimSpace(entries[len(entries)-1])); ip != nil {
			return ip.String()
		}
	}
	return remote
}

func (s *Server) trustedProxy(remote string) bool {
	ip := net.ParseIP(remote)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, n := range s.cfg.TrustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// warnClient logs a failed login or pairing attempt, at most once per
// minute and client (/64 for IPv6) with the number of suppressed repeats.
func (s *Server) warnClient(msg, ip string, attrs ...any) {
	ok, suppressed := s.warnings.allow(msg + "|" + limiterKey(ip))
	if !ok {
		return
	}
	attrs = append([]any{"ip", ip}, attrs...)
	if suppressed > 0 {
		attrs = append(attrs, "suppressedSinceLast", suppressed)
	}
	s.log.Warn(msg, attrs...)
}

// readEnvelope reads one sealed JSON message within timeout. Audio frames
// before it are an error.
func readEnvelope(ctx context.Context, conn *hp2.Conn, timeout time.Duration) (protocol.Envelope, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	typ, plaintext, err := conn.Read(ctx)
	if err != nil {
		return protocol.Envelope{}, err
	}
	if typ != hp2.FrameJSON {
		return protocol.Envelope{}, errors.New("JSON message expected")
	}
	return protocol.ParseEnvelope(plaintext[1:])
}

func writeEnvelope(ctx context.Context, conn *hp2.Conn, env protocol.Envelope) error {
	data, err := env.Marshal()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.WriteJSON(ctx, data)
}

func writeAudio(ctx context.Context, conn *hp2.Conn, frame []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.WriteAudio(ctx, frame)
}

func errorEnvelope(code, message string) protocol.Envelope {
	return protocol.MustEnvelope(protocol.TypeError, protocol.Error{Code: code, Message: message})
}

// pairError is a failed pairing attempt.
type pairError struct {
	code    string
	message string
	status  int
}

var (
	errPairRateLimited = &pairError{protocol.ErrorPairingRateLimited, "Zu viele Versuche, bitte später erneut probieren", http.StatusTooManyRequests}
	errPairInvalid     = &pairError{protocol.ErrorPairingInvalid, "Der Kopplungscode ist ungültig oder abgelaufen", http.StatusForbidden}
	errPairInternal    = &pairError{protocol.ErrorInternal, "internal error", http.StatusInternalServerError}
)

// pair validates a pairing request (v2): rate limit, well-formed fields,
// the device's proof of key possession, then the one-time code. It stores
// the new device with its public key and answers with the bridge's
// signature over the pairing.
func (s *Server) pair(req protocol.PairRequest, decodeErr error, ip string) (protocol.PairResponse, *pairError) {
	if s.limiter.blocked(ip) {
		s.warnClient("pairing rate limited", ip)
		return protocol.PairResponse{}, errPairRateLimited
	}
	// Malformed requests are 400 and do not count towards the rate limit
	// (a buggy app must not lock itself out); wrong codes and proofs do.
	if decodeErr != nil || strings.TrimSpace(req.Code) == "" || !validPlatform(req.Platform) {
		return protocol.PairResponse{}, &pairError{protocol.ErrorBadRequest, "code, deviceName, platform (ios|watchos), publicKey, nonce and proof are required", http.StatusBadRequest}
	}
	if _, err := hp2.ParseDevicePublicKey(req.PublicKey); err != nil {
		return protocol.PairResponse{}, &pairError{protocol.ErrorBadRequest, "publicKey must be an uncompressed P-256 key", http.StatusBadRequest}
	}
	// The proof is checked before the code is consumed, so a request with
	// a forged key cannot burn somebody's code.
	code, err := hp2.VerifyPairProof(req)
	if err != nil {
		s.limiter.fail(ip)
		s.warnClient("pairing proof rejected", ip)
		return protocol.PairResponse{}, errPairInvalid
	}
	now := s.cfg.Now()
	pc, err := s.cfg.Pairing.Consume(code, now)
	if err == nil {
		err = s.checkCompanionCode(pc, req.Platform)
	}
	if err != nil {
		s.limiter.fail(ip)
		s.warnClient("pairing failed", ip, "error", err)
		if errors.Is(err, store.ErrPairingInvalid) {
			return protocol.PairResponse{}, errPairInvalid
		}
		return protocol.PairResponse{}, errPairInternal
	}

	name := sanitizeName(pc.Name)
	if name == "" {
		name = sanitizeName(req.DeviceName)
	}
	if name == "" {
		name = req.Platform
	}
	// ADR-0008: a device joins the profile chosen for its code; a watch
	// joins the profile of the iPhone that requested its code.
	profileID := pc.Profile
	if pc.ParentID != "" {
		parent, err := s.cfg.Devices.Get(pc.ParentID)
		if err != nil {
			s.log.Error("companion parent lookup failed", "error", err)
			return protocol.PairResponse{}, errPairInternal
		}
		profileID = parent.Profile
	}
	dev := store.Device{
		ID:        uuid.NewString(),
		Name:      name,
		Platform:  req.Platform,
		Model:     sanitizeName(req.Model),
		PublicKey: req.PublicKey,
		PairedBy:  pc.ParentID,
		Profile:   profileID,
		CreatedAt: now.UTC(),
	}
	dev, err = s.cfg.Devices.AddPromotingFirst(dev, now.Add(AdminEnrollWindow).UTC())
	if err != nil {
		s.log.Error("storing device failed", "error", err)
		return protocol.PairResponse{}, errPairInternal
	}
	if dev.Admin {
		s.log.Info("first device of the bridge is admin", "device", dev.ID)
	}
	if err := s.cfg.Pairing.RecordUse(pc.Code, dev.ID, now); err != nil {
		s.log.Warn("recording code use failed", "error", err)
	}
	s.log.Info("device paired", "device", dev.ID, "name", dev.Name, "platform", dev.Platform, "model", dev.Model,
		"key", hp2.KeyFingerprint(dev.PublicKey), "ip", ip)
	s.broadcastDevicePaired(dev, now)
	return hp2.SignPairResponse(s.cfg.Identity, req, code, protocol.PairResponse{
		DeviceID:   dev.ID,
		BridgeID:   s.cfg.BridgeID,
		BridgeName: s.cfg.BridgeName,
	}), nil
}

// broadcastDevicePaired tells the other connected devices of the same
// profile about a new pairing, so an unexpected one does not go unnoticed.
// Other profiles don't learn about each other's devices (ADR-0008).
func (s *Server) broadcastDevicePaired(dev store.Device, now time.Time) {
	env := protocol.MustEnvelope(protocol.TypeDevicePaired, protocol.DevicePaired{
		DeviceName: dev.Name,
		Platform:   dev.Platform,
		PairedAt:   protocol.Timestamp(now),
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if id != dev.ID && sess.profileID == dev.ProfileID() {
			sess.Send(env)
		}
	}
}

// checkCompanionCode enforces the restrictions of a companion code: only its
// platform may use it, and the device that requested it must still be
// paired, so a code requested just before that device was removed is void.
func (s *Server) checkCompanionCode(pc store.PairingCode, platform string) error {
	if pc.Platform != "" && pc.Platform != platform {
		return fmt.Errorf("%w: code is for platform %s", store.ErrPairingInvalid, pc.Platform)
	}
	if pc.ParentID == "" {
		return nil
	}
	if _, err := s.cfg.Devices.Get(pc.ParentID); err != nil {
		if errors.Is(err, store.ErrDeviceNotFound) {
			return fmt.Errorf("%w: requesting device was removed", store.ErrPairingInvalid)
		}
		return err
	}
	return nil
}

func validPlatform(p string) bool {
	return p == protocol.PlatformIOS || p == protocol.PlatformWatchOS
}

// sanitizeName cleans a device name or model from a client: control
// characters (terminal escapes, line breaks) and bidi overrides are removed,
// so a device cannot hide or disguise entries in devices list or the logs;
// the result is trimmed and cut to maxDeviceNameRunes.
func sanitizeName(s string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || isBidiControl(r) {
			return -1
		}
		return r
	}, s)
	return truncateRunes(strings.TrimSpace(clean), maxDeviceNameRunes)
}

// isBidiControl reports embedding, override and isolate controls as well as
// the directional marks.
func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0x200E || r == 0x200F || r == 0x061C
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// validPushKey accepts a base64url X25519 public key (ADR-0010).
func validPushKey(key string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(key)
	return err == nil && pushseal.ValidKey(raw)
}

// validPushToken accepts lower-case hex tokens.
func validPushToken(token string) bool {
	if token == "" || len(token) > maxPushTokenLength || len(token)%2 != 0 || strings.ToLower(token) != token {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func validEnvironment(env string) bool {
	return env == protocol.PushEnvironmentDevelopment || env == protocol.PushEnvironmentProduction
}

// Shutdown closes every device connection and waits until their handlers
// finished (bounded by ctx).
func (s *Server) Shutdown(ctx context.Context) {
	s.CloseAll()
	done := make(chan struct{})
	go func() {
		s.active.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// CloseAll closes every device connection.
func (s *Server) CloseAll() {
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.close(websocket.StatusGoingAway, "bridge shutting down")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
