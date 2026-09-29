// Package signaling serves the device WebSocket (/v1/ws), the health
// endpoint (/v1/health) and the HTTPS device endpoints of v1.1
// (/v1/pair, /v1/device) of the signaling protocol.
package signaling

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/auth"
	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Hub receives authenticated device traffic (implemented by calls.Manager).
type Hub interface {
	DeviceConnected(calls.DeviceConn)
	DeviceDisconnected(calls.DeviceConn)
	HandleDeviceMessage(calls.DeviceConn, protocol.Envelope)
	SIPRegistered() bool
	// CallStatus answers GET /v1/calls/{callId} for one device (v1.1).
	CallStatus(deviceID, callID string) (protocol.CallStatus, bool)
}

// Config configures the server.
type Config struct {
	BridgeID      string
	BridgeName    string
	BridgeVersion string
	// PublicURL is the wss:// URL sent in pair.companion (v1.1).
	PublicURL string
	// PushTopic is the configured APNs topic; per-device topics must share
	// its bundle prefix (v1.1).
	PushTopic string
	// TrustProxyHeaders uses CF-Connecting-IP / X-Forwarded-For.
	TrustProxyHeaders bool
	Devices           *store.Devices
	Pairing           *store.Pairing
	Hub               Hub
	Logger            *slog.Logger

	PingInterval     time.Duration
	FirstMessageWait time.Duration
	Now              func() time.Time
}

// Limits.
const (
	maxMessageBytes    = 64 << 10
	sendQueue          = 64
	writeTimeout       = 10 * time.Second
	closeGrace         = time.Second
	maxDeviceNameRunes = 64
	maxPushTokenLength = 200
)

// Server is the HTTP handler for /v1/*.
type Server struct {
	cfg     Config
	log     *slog.Logger
	limiter *rateLimiter

	mu       sync.Mutex
	sessions map[string]*session
	active   sync.WaitGroup
}

// New creates a server.
func New(cfg Config) *Server {
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 20 * time.Second
	}
	if cfg.FirstMessageWait == 0 {
		cfg.FirstMessageWait = 10 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{
		cfg:      cfg,
		log:      cfg.Logger.With("component", "signaling"),
		limiter:  newRateLimiter(5, time.Minute, cfg.Now),
		sessions: map[string]*session{},
	}
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("GET /v1/ws", s.websocket)
	mux.HandleFunc("POST /v1/pair", s.httpPair)
	mux.HandleFunc("PUT /v1/device", s.httpUpdateDevice)
	mux.HandleFunc("DELETE /v1/device", s.httpDeleteDevice)
	mux.HandleFunc("GET /v1/calls/{callId}", s.httpCallStatus)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"version":       s.cfg.BridgeVersion,
		"sipRegistered": s.cfg.Hub.SIPRegistered(),
	})
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	s.active.Add(1)
	defer s.active.Done()
	ip := s.clientIP(r)
	header := r.Header.Get("Authorization")
	if header == "" {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.SetReadLimit(maxMessageBytes)
		s.runPairing(r.Context(), conn, ip)
		return
	}

	dev, ok := s.authenticate(header)
	if !ok {
		s.log.Warn("rejected WebSocket authentication", "ip", ip)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(maxMessageBytes)
	s.runDevice(r.Context(), conn, dev, ip)
}

func (s *Server) authenticate(header string) (store.Device, bool) {
	id, secret, ok := auth.ParseBearer(header)
	if !ok {
		return store.Device{}, false
	}
	dev, err := s.cfg.Devices.Get(id)
	if err != nil {
		return store.Device{}, false
	}
	return dev, auth.VerifySecret(secret, dev.SecretHash)
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxyHeaders {
		if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			first, _, _ := strings.Cut(fwd, ",")
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// readEnvelope reads one text message within timeout.
func readEnvelope(ctx context.Context, conn *websocket.Conn, timeout time.Duration) (protocol.Envelope, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return protocol.Envelope{}, err
	}
	if typ != websocket.MessageText {
		return protocol.Envelope{}, errors.New("binary messages are not supported")
	}
	return protocol.ParseEnvelope(data)
}

func writeEnvelope(ctx context.Context, conn *websocket.Conn, env protocol.Envelope) error {
	data, err := env.Marshal()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}

func writeBinary(ctx context.Context, conn *websocket.Conn, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageBinary, data)
}

func errorEnvelope(code, message string) protocol.Envelope {
	return protocol.MustEnvelope(protocol.TypeError, protocol.Error{Code: code, Message: message})
}

// runPairing handles an unauthenticated connection: exactly one pair message.
func (s *Server) runPairing(ctx context.Context, conn *websocket.Conn, ip string) {
	defer conn.CloseNow()
	env, err := readEnvelope(ctx, conn, s.cfg.FirstMessageWait)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "pair expected")
		return
	}
	if env.Type != protocol.TypePair {
		_ = writeEnvelope(ctx, conn, errorEnvelope(protocol.ErrorUnauthorized, "authentication required"))
		_ = conn.Close(websocket.StatusPolicyViolation, "unauthorized")
		return
	}
	var p protocol.Pair
	decodeErr := env.Decode(&p)
	ok, pairErr := s.pair(p, decodeErr, ip)
	if pairErr != nil {
		_ = writeEnvelope(ctx, conn, errorEnvelope(pairErr.code, pairErr.message))
		_ = conn.Close(websocket.StatusPolicyViolation, pairErr.closeReason())
		return
	}
	_ = writeEnvelope(ctx, conn, protocol.MustEnvelope(protocol.TypePairOK, ok))
	_ = conn.Close(websocket.StatusNormalClosure, "paired")
}

// pairError is a failed pairing attempt (WebSocket pair or POST /v1/pair).
type pairError struct {
	code    string
	message string
	status  int
}

func (e *pairError) closeReason() string {
	switch e.code {
	case protocol.ErrorPairingRateLimited:
		return "rate limited"
	case protocol.ErrorBadRequest:
		return "bad request"
	}
	return "pairing failed"
}

// pair validates a pairing request, consumes its code and stores the new
// device. Rate limiting, code rules and naming are the same for WebSocket
// and HTTPS pairing.
func (s *Server) pair(p protocol.Pair, decodeErr error, ip string) (protocol.PairOK, *pairError) {
	if s.limiter.blocked(ip) {
		s.log.Warn("pairing rate limited", "ip", ip)
		return protocol.PairOK{}, &pairError{protocol.ErrorPairingRateLimited, "Zu viele Versuche, bitte später erneut probieren", http.StatusTooManyRequests}
	}
	if decodeErr != nil || strings.TrimSpace(p.Code) == "" || !validPlatform(p.Platform) {
		return protocol.PairOK{}, &pairError{protocol.ErrorBadRequest, "code, deviceName and platform (ios|watchos) are required", http.StatusBadRequest}
	}
	now := s.cfg.Now()
	pc, err := s.cfg.Pairing.Consume(p.Code, now)
	if err != nil {
		s.limiter.fail(ip)
		s.log.Warn("pairing failed", "ip", ip, "error", err)
		if errors.Is(err, store.ErrPairingInvalid) {
			return protocol.PairOK{}, &pairError{protocol.ErrorPairingInvalid, "Der Kopplungscode ist ungültig oder abgelaufen", http.StatusForbidden}
		}
		return protocol.PairOK{}, &pairError{protocol.ErrorInternal, "internal error", http.StatusInternalServerError}
	}

	secret, err := auth.NewSecret()
	if err != nil {
		return protocol.PairOK{}, &pairError{protocol.ErrorInternal, "internal error", http.StatusInternalServerError}
	}
	name := strings.TrimSpace(pc.Name)
	if name == "" {
		name = strings.TrimSpace(p.DeviceName)
	}
	if name == "" {
		name = p.Platform
	}
	dev := store.Device{
		ID:         uuid.NewString(),
		Name:       truncateRunes(name, maxDeviceNameRunes),
		Platform:   p.Platform,
		Model:      truncateRunes(p.Model, maxDeviceNameRunes),
		SecretHash: auth.HashSecret(secret),
		CreatedAt:  now.UTC(),
	}
	if err := s.cfg.Devices.Add(dev); err != nil {
		s.log.Error("storing device failed", "error", err)
		return protocol.PairOK{}, &pairError{protocol.ErrorInternal, "internal error", http.StatusInternalServerError}
	}
	s.log.Info("device paired", "device", dev.ID, "name", dev.Name, "platform", dev.Platform, "ip", ip)
	return protocol.PairOK{
		DeviceID:     dev.ID,
		DeviceSecret: secret,
		BridgeID:     s.cfg.BridgeID,
		BridgeName:   s.cfg.BridgeName,
	}, nil
}

func validPlatform(p string) bool {
	return p == protocol.PlatformIOS || p == protocol.PlatformWatchOS
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
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
