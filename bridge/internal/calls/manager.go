package calls

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/logsafe"
	"github.com/JoKeks2023/housephone/bridge/internal/profile"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Defaults for Options.
const (
	DefaultReattachTimeout = 30 * time.Second
	DefaultTombstoneTTL    = 2 * time.Minute
	DefaultOfferTimeout    = 5 * time.Second
	DefaultAnswerTimeout   = 35 * time.Second
	DefaultPushTimeout     = 10 * time.Second
	// DefaultOfferAnswerTimeout ends an outgoing call whose device never
	// answers the offer.
	DefaultOfferAnswerTimeout = 15 * time.Second
	// DefaultMediaFallbackTimeout is how long a device's WebRTC path may
	// take to connect before the call falls back to WebSocket audio.
	DefaultMediaFallbackTimeout = 5 * time.Second
	// DefaultMaxCalls bounds concurrent calls when dialing out.
	DefaultMaxCalls = 8
	// MaxOutgoingPerDevice bounds concurrent outgoing calls per device.
	MaxOutgoingPerDevice = 2
)

var (
	numberPattern = regexp.MustCompile(`^\+?[0-9*#]{1,32}$`)
	dtmfPattern   = regexp.MustCompile(`^[0-9*#]{1,32}$`)
)

// Options configure a Manager.
type Options struct {
	BridgeID   string
	ICEServers []protocol.ICEServer
	SIP        SIPLeg
	Peers      PeerFactory
	// Pusher may be nil when APNs is not configured.
	Pusher  Pusher
	Devices DeviceDirectory
	Logger  *slog.Logger
	// CallerNames looks up a caller number in the FRITZ!Box phonebook when
	// the INVITE carries no display name (v1.2), in the phonebooks of the
	// profile whose line rings (ADR-0008). It must not block; nil disables
	// the lookup.
	CallerNames func(profileID, number string) string

	ReattachTimeout time.Duration
	TombstoneTTL    time.Duration
	OfferTimeout    time.Duration
	AnswerTimeout   time.Duration
	PushTimeout     time.Duration
	// OfferAnswerTimeout ends an outgoing call if the device does not
	// answer the offer in time.
	OfferAnswerTimeout time.Duration
	// MediaFallbackTimeout: a device that can fall back to WebSocket audio
	// gets it when its WebRTC path is not connected this long after the
	// answer (signaling v1.4).
	MediaFallbackTimeout time.Duration
	// MaxCalls bounds the concurrent calls; call.dial beyond it is refused.
	MaxCalls int
	// OnEvent receives call events (statistics, admin TUI). It must not
	// block; nil disables them.
	OnEvent func(Event)
	// AudioFrameInterval paces websocket-pcma audio towards the FRITZ!Box
	// (default 20 ms; tests may shorten it).
	AudioFrameInterval time.Duration
	Now                func() time.Time
}

type tombstone struct {
	profile string
	reason  string
	sipCode int
	at      time.Time
	// statuses: the final status per participating device.
	statuses map[string]protocol.CallStatus
}

// Manager routes device messages and SIP events to per-call actors.
type Manager struct {
	opts Options
	log  *slog.Logger

	mu sync.Mutex
	// lines are the SIP legs of the profiles beyond the default one
	// (opts.SIP).
	lines  map[string]SIPLeg
	conns  map[string]DeviceConn
	calls  map[string]*call
	tombs  map[string]tombstone
	closed bool
	wg     sync.WaitGroup
}

// NewManager creates a Manager. SIP may be set later with SetSIP.
func NewManager(opts Options) *Manager {
	if opts.ReattachTimeout == 0 {
		opts.ReattachTimeout = DefaultReattachTimeout
	}
	if opts.TombstoneTTL == 0 {
		opts.TombstoneTTL = DefaultTombstoneTTL
	}
	if opts.OfferTimeout == 0 {
		opts.OfferTimeout = DefaultOfferTimeout
	}
	if opts.AnswerTimeout == 0 {
		opts.AnswerTimeout = DefaultAnswerTimeout
	}
	if opts.PushTimeout == 0 {
		opts.PushTimeout = DefaultPushTimeout
	}
	if opts.MediaFallbackTimeout == 0 {
		opts.MediaFallbackTimeout = DefaultMediaFallbackTimeout
	}
	if opts.OfferAnswerTimeout == 0 {
		opts.OfferAnswerTimeout = DefaultOfferAnswerTimeout
	}
	if opts.MaxCalls == 0 {
		opts.MaxCalls = DefaultMaxCalls
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.ICEServers == nil {
		opts.ICEServers = []protocol.ICEServer{}
	}
	return &Manager{
		opts:  opts,
		log:   opts.Logger.With("component", "calls"),
		conns: map[string]DeviceConn{},
		calls: map[string]*call{},
		tombs: map[string]tombstone{},
	}
}

// SetSIP sets the SIP leg of the default profile (it needs the manager as
// its call handler, so it is created afterwards).
func (m *Manager) SetSIP(sip SIPLeg) {
	m.SetLine(profile.DefaultID, sip)
}

// SetLine sets the SIP leg of a profile (ADR-0008).
func (m *Manager) SetLine(profileID string, sip SIPLeg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	profileID = profile.Normalize(profileID)
	if profileID == profile.DefaultID {
		m.opts.SIP = sip
		return
	}
	if m.lines == nil {
		m.lines = map[string]SIPLeg{}
	}
	m.lines[profileID] = sip
}

// sipFor returns the SIP leg of a profile, nil if it has none.
func (m *Manager) sipFor(profileID string) SIPLeg {
	m.mu.Lock()
	defer m.mu.Unlock()
	profileID = profile.Normalize(profileID)
	if profileID == profile.DefaultID {
		return m.opts.SIP
	}
	return m.lines[profileID]
}

// SIPRegistered reports whether every line is registered (health).
func (m *Manager) SIPRegistered() bool {
	m.mu.Lock()
	legs := []SIPLeg{m.opts.SIP}
	for _, l := range m.lines {
		legs = append(legs, l)
	}
	m.mu.Unlock()
	for _, l := range legs {
		if l == nil || !l.Registered() {
			return false
		}
	}
	return true
}

// SIPRegisteredFor reports the registration of one profile's line, for its
// devices' welcome.
func (m *Manager) SIPRegisteredFor(profileID string) bool {
	sip := m.sipFor(profileID)
	return sip != nil && sip.Registered()
}

// profileOf is the profile of a connected device.
func profileOf(conn DeviceConn) string { return profile.Normalize(conn.ProfileID()) }

// DeviceConnected registers an authenticated connection after hello. The
// signaling server guarantees one connection per device.
func (m *Manager) DeviceConnected(conn DeviceConn) {
	m.mu.Lock()
	m.conns[conn.DeviceID()] = conn
	m.mu.Unlock()
}

// DeviceDisconnected unregisters a connection and informs active calls.
func (m *Manager) DeviceDisconnected(conn DeviceConn) {
	m.mu.Lock()
	if current, ok := m.conns[conn.DeviceID()]; ok && current == conn {
		delete(m.conns, conn.DeviceID())
	}
	calls := m.activeCallsLocked()
	m.mu.Unlock()
	for _, c := range calls {
		c.do(func() { c.onDisconnect(conn) })
	}
}

// usesWebSocketAudio reports whether the device takes calls over the
// websocket-pcma media path (v1.1). Unknown devices use WebRTC.
// DeviceRevoked ends a removed device's part in every call, as if it hung
// up: a declined ringing call, a BYE/CANCEL where it is the active device.
// It is called when the device was removed while still connected.
func (m *Manager) DeviceRevoked(conn DeviceConn) {
	m.mu.Lock()
	calls := m.activeCallsLocked()
	m.mu.Unlock()
	for _, c := range calls {
		c.do(func() { c.onRevoked(conn) })
	}
}

func (m *Manager) usesWebSocketAudio(deviceID string) bool {
	dev, err := m.opts.Devices.Get(deviceID)
	return err == nil && dev.UsesWebSocketAudio()
}

// canFallBack reports a WebRTC device that also takes websocket-pcma
// audio (v1.4).
func (m *Manager) canFallBack(deviceID string) bool {
	dev, err := m.opts.Devices.Get(deviceID)
	return err == nil && dev.CanFallBackToWebSocketAudio()
}

func (m *Manager) conn(deviceID string) DeviceConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conns[deviceID]
}

// BroadcastStatus sends the default line's registration state to the
// default profile's devices.
func (m *Manager) BroadcastStatus(registered bool) {
	m.BroadcastProfileStatus(profile.DefaultID, registered)
}

// BroadcastProfileStatus sends a line's registration state to the devices
// of its profile.
func (m *Manager) BroadcastProfileStatus(profileID string, registered bool) {
	profileID = profile.Normalize(profileID)
	env := protocol.MustEnvelope(protocol.TypeStatus, protocol.Status{SIPRegistered: registered})
	m.mu.Lock()
	conns := make([]DeviceConn, 0, len(m.conns))
	for _, c := range m.conns {
		if profileOf(c) == profileID {
			conns = append(conns, c)
		}
	}
	m.mu.Unlock()
	for _, c := range conns {
		c.Send(env)
	}
}

// HandleDeviceMessage handles call.* messages from an authenticated device.
// Other message types are ignored (they belong to the signaling server).
func (m *Manager) HandleDeviceMessage(conn DeviceConn, env protocol.Envelope) {
	switch env.Type {
	case protocol.TypeCallAttach:
		var p protocol.CallAttach
		if id, ok := m.decodeCallID(conn, env, &p, &p.CallID); ok {
			m.withCall(conn, id, true, func(c *call) { c.onAttach(conn) })
		}
	case protocol.TypeCallDial:
		var p protocol.CallDial
		if id, ok := m.decodeCallID(conn, env, &p, &p.CallID); ok {
			m.startOutgoing(conn, id, p.Number)
		}
	case protocol.TypeCallAnswer:
		var p protocol.CallAnswer
		if id, ok := m.decodeCallID(conn, env, &p, &p.CallID); ok {
			if strings.TrimSpace(p.SDP) == "" {
				sendError(conn, protocol.ErrorBadRequest, "sdp is required", id)
				return
			}
			m.withCall(conn, id, false, func(c *call) { c.onAnswer(conn, p.SDP) })
		}
	case protocol.TypeCallAccept:
		var p protocol.CallAccept
		if id, ok := m.decodeCallID(conn, env, &p, &p.CallID); ok {
			m.withCall(conn, id, true, func(c *call) { c.onAccept(conn) })
		}
	case protocol.TypeCallHangup:
		var p protocol.CallHangup
		if id, ok := m.decodeCallID(conn, env, &p, &p.CallID); ok {
			m.withCall(conn, id, true, func(c *call) { c.onHangup(conn, p.Reason) })
		}
	case protocol.TypeCallDTMF:
		var p protocol.CallDTMF
		if id, ok := m.decodeCallID(conn, env, &p, &p.CallID); ok {
			if !dtmfPattern.MatchString(p.Digits) {
				sendError(conn, protocol.ErrorBadRequest, "digits must match [0-9*#]{1,32}", id)
				return
			}
			m.withCall(conn, id, false, func(c *call) { c.onDTMF(conn, p.Digits) })
		}
	}
}

// decodeCallID decodes the payload and normalizes the call ID to a lower-case
// UUID.
func (m *Manager) decodeCallID(conn DeviceConn, env protocol.Envelope, payload any, callID *string) (string, bool) {
	if err := env.Decode(payload); err != nil {
		sendError(conn, protocol.ErrorBadRequest, err.Error(), "")
		return "", false
	}
	id, ok := normalizeCallID(*callID)
	if !ok {
		sendError(conn, protocol.ErrorBadRequest, "callId must be a UUID", "")
		return "", false
	}
	return id, true
}

func normalizeCallID(raw string) (string, bool) {
	parsed, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}

// withCall runs fn on the call's actor. Unknown calls are answered with
// call.ended (using a tombstone reason if the call ended recently) when
// endedIfMissing is set, otherwise with error call_not_found.
func (m *Manager) withCall(conn DeviceConn, id string, endedIfMissing bool, fn func(*call)) {
	m.mu.Lock()
	c := m.calls[id]
	tomb, hasTomb := m.tombs[id]
	m.mu.Unlock()

	// A call of another profile does not exist for this device (ADR-0008):
	// it could otherwise attach to a ringing call it was never offered.
	own := profileOf(conn)
	if c != nil && c.profile != own {
		c = nil
	}
	if hasTomb && tomb.profile != own {
		hasTomb = false
	}
	if c != nil && c.do(func() { fn(c) }) {
		return
	}
	if !endedIfMissing {
		sendError(conn, protocol.ErrorCallNotFound, "unknown or ended call", id)
		return
	}
	reason, code := protocol.EndReasonNotFound, 0
	if hasTomb {
		reason, code = tomb.reason, tomb.sipCode
	}
	conn.Send(protocol.MustEnvelope(protocol.TypeCallEnded, protocol.CallEnded{CallID: id, Reason: reason, SIPCode: code}))
}

// CallStatus reports a call from one device's point of view for GET
// /v1/calls/{callId} (v1.1). ok is false if the call is unknown (also once
// its tombstone expired) or the device did not take part in it.
func (m *Manager) CallStatus(deviceID, rawCallID string) (protocol.CallStatus, bool) {
	id, valid := normalizeCallID(rawCallID)
	if !valid {
		return protocol.CallStatus{}, false
	}
	m.mu.Lock()
	c := m.calls[id]
	tomb, hasTomb := m.tombs[id]
	m.mu.Unlock()

	if c != nil {
		type result struct {
			st protocol.CallStatus
			ok bool
		}
		answer := make(chan result, 1)
		if c.do(func() {
			st, ok := c.statusFor(deviceID)
			answer <- result{st, ok}
		}) {
			select {
			case r := <-answer:
				return r.st, r.ok
			case <-c.done:
			}
		}
		// The actor stopped before answering; its state is final now.
		<-c.done
		return c.statusFor(deviceID)
	}
	if hasTomb && m.opts.Now().Sub(tomb.at) <= m.opts.TombstoneTTL {
		st, ok := tomb.statuses[deviceID]
		return st, ok
	}
	return protocol.CallStatus{}, false
}

var errTooManyCalls = errors.New("too many calls")

// addOutgoingCall adds a call dialed by c.dialer unless that device already
// has MaxOutgoingPerDevice outgoing calls or the bridge MaxCalls calls
// (security review N3: a device could otherwise open calls without end).
func (m *Manager) addOutgoingCall(c *call) error {
	m.mu.Lock()
	if len(m.calls) >= m.opts.MaxCalls {
		m.mu.Unlock()
		return errTooManyCalls
	}
	own := 0
	for _, other := range m.calls {
		if other.dir == directionOutgoing && other.dialer == c.dialer {
			own++
		}
	}
	m.mu.Unlock()
	if own >= MaxOutgoingPerDevice {
		return errTooManyCalls
	}
	if !m.addCall(c) {
		return errors.New("call exists")
	}
	return nil
}

func (m *Manager) addCall(c *call) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	if _, exists := m.calls[c.id]; exists {
		return false
	}
	if _, ended := m.tombs[c.id]; ended {
		return false
	}
	m.calls[c.id] = c
	m.wg.Add(1)
	return true
}

func (m *Manager) removeCall(c *call) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls[c.id] == c {
		delete(m.calls, c.id)
	}
	now := m.opts.Now()
	for id, t := range m.tombs {
		if now.Sub(t.at) > m.opts.TombstoneTTL {
			delete(m.tombs, id)
		}
	}
	// The actor has stopped (removeCall runs after c.run returned), so its
	// state is final and safe to read here.
	statuses := make(map[string]protocol.CallStatus, len(c.participants))
	for id := range c.participants {
		if st, ok := c.statusFor(id); ok {
			statuses[id] = st
		}
	}
	m.tombs[c.id] = tombstone{profile: c.profile, reason: c.endReason, sipCode: c.endSIPCode, at: now, statuses: statuses}
	m.wg.Done()
}

func (m *Manager) activeCallsLocked() []*call {
	out := make([]*call, 0, len(m.calls))
	for _, c := range m.calls {
		out = append(out, c)
	}
	return out
}

// ActiveCalls returns the number of calls in progress.
func (m *Manager) ActiveCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// HandleIncoming runs an incoming call on the default profile's line.
func (m *Manager) HandleIncoming(ctx context.Context, sip IncomingSIPCall) {
	m.HandleIncomingOn(ctx, profile.DefaultID, sip)
}

// HandleIncomingFor returns the INVITE handler of a profile's SIP leg.
func (m *Manager) HandleIncomingFor(profileID string) func(context.Context, IncomingSIPCall) {
	return func(ctx context.Context, sip IncomingSIPCall) { m.HandleIncomingOn(ctx, profileID, sip) }
}

// HandleIncomingOn runs an incoming call until its SIP dialog ends. It is
// called by the SIP leg of profileID for every INVITE; only that profile's
// devices ring and get a push (ADR-0008).
func (m *Manager) HandleIncomingOn(ctx context.Context, profileID string, sip IncomingSIPCall) {
	profileID = profile.Normalize(profileID)
	c := newCall(m, uuid.NewString(), directionIncoming, profileID)
	c.caller, c.callerName, c.codec = sip.Caller(), sip.CallerName(), sip.Codec()
	if c.callerName == "" && c.caller != "" && m.opts.CallerNames != nil {
		c.callerName = m.opts.CallerNames(profileID, c.caller)
	}
	c.sipIn = sip
	log := c.log.With("caller", logsafe.Number(c.caller), logsafe.CallerName(c.callerName), "codec", c.codec)

	all, err := m.opts.Devices.List()
	if err != nil {
		log.Error("listing devices failed", "error", err)
	}
	devices := make([]store.Device, 0, len(all))
	for _, dev := range all {
		if dev.ProfileID() == profileID {
			devices = append(devices, dev)
		}
	}
	if !m.addCall(c) {
		_ = sip.Reject(503, "Service Unavailable")
		return
	}
	defer m.removeCall(c)

	pcmaOffered := sip.Offers(codec.PCMA)
	reachable := 0
	for _, dev := range devices {
		if dev.UsesWebSocketAudio() && !pcmaOffered {
			// The watch only speaks PCMA and the bridge never transcodes.
			log.Info("not ringing websocket-pcma device: INVITE offers no PCMA", "device", dev.ID)
			continue
		}
		online := m.conn(dev.ID)
		canPush := m.opts.Pusher != nil && dev.PushToken != ""
		if online == nil && !canPush {
			continue
		}
		reachable++
		c.notified[dev.ID] = true
		c.participants[dev.ID] = true
		if canPush {
			m.push(c, dev)
		}
		if online != nil {
			c.informed[dev.ID] = online
			online.Send(c.incomingEnvelope())
		}
	}
	log.Info("incoming call", "devices", len(devices), "reachable", reachable)
	m.emit(c.event(EventStarted))
	if reachable == 0 {
		c.endReason = protocol.EndReasonFailed
		_ = sip.Reject(480, "Temporarily Unavailable")
		return
	}
	if err := sip.Ringing(); err != nil {
		log.Warn("sending 180 Ringing failed", "error", err)
	}

	go func() {
		select {
		case <-sip.Done():
			c.do(c.onSIPEnded)
		case <-c.done:
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			c.do(c.onShutdown)
		case <-c.done:
		}
	}()
	c.run()
	c.waitAnswer()
}

func (m *Manager) push(c *call, dev store.Device) {
	payload := protocol.NewPushIncomingCall(c.id, c.caller, c.callerName, m.opts.BridgeID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), m.opts.PushTimeout)
		defer cancel()
		err := m.opts.Pusher.PushIncomingCall(ctx, dev, payload)
		if err == nil {
			m.emit(Event{Kind: EventPushOK, CallID: c.id, DeviceID: dev.ID})
			return
		}
		m.emit(Event{Kind: EventPushFailed, CallID: c.id, DeviceID: dev.ID, Error: err.Error()})
		m.log.Warn("push failed", "device", dev.ID, "call", c.id, "error", err)
		if isInvalidToken(err) {
			if clearErr := m.opts.Devices.ClearPushToken(dev.ID, dev.PushToken); clearErr != nil {
				m.log.Error("clearing push token failed", "device", dev.ID, "error", clearErr)
			}
		}
		c.do(func() { c.onPushFailed(dev.ID) })
	}()
}

func (m *Manager) startOutgoing(conn DeviceConn, id, number string) {
	if !numberPattern.MatchString(number) {
		sendError(conn, protocol.ErrorInvalidNumber, "number must match \\+?[0-9*#]{1,32}", id)
		conn.Send(protocol.MustEnvelope(protocol.TypeCallEnded, protocol.CallEnded{CallID: id, Reason: protocol.EndReasonFailed}))
		return
	}
	// Out over the device's own line, so the callee sees its number.
	own := profileOf(conn)
	if !m.SIPRegisteredFor(own) {
		sendError(conn, protocol.ErrorSIPUnavailable, "Bridge ist nicht an der FRITZ!Box registriert", id)
		conn.Send(protocol.MustEnvelope(protocol.TypeCallEnded, protocol.CallEnded{CallID: id, Reason: protocol.EndReasonFailed}))
		return
	}
	c := newCall(m, id, directionOutgoing, own)
	c.number, c.caller = number, number
	c.participants[conn.DeviceID()] = true
	c.dialer = conn.DeviceID()
	switch err := m.addOutgoingCall(c); {
	case errors.Is(err, errTooManyCalls):
		sendError(conn, protocol.ErrorTooManyCalls, "Zu viele gleichzeitige Anrufe", id)
		conn.Send(protocol.MustEnvelope(protocol.TypeCallEnded, protocol.CallEnded{CallID: id, Reason: protocol.EndReasonFailed}))
		return
	case err != nil:
		sendError(conn, protocol.ErrorBadRequest, "callId already used", id)
		return
	}
	go func() {
		defer m.removeCall(c)
		c.run()
	}()
	c.do(func() { c.startOutgoing(conn) })
}

// Shutdown ends all calls and waits for them to finish (bounded by ctx).
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	m.closed = true
	calls := m.activeCallsLocked()
	m.mu.Unlock()
	for _, c := range calls {
		c.do(c.onShutdown)
	}
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func isInvalidToken(err error) bool {
	return errors.Is(err, ErrInvalidPushToken)
}

func sendError(conn DeviceConn, code, message, callID string) {
	conn.Send(protocol.MustEnvelope(protocol.TypeError, protocol.Error{Code: code, Message: message, CallID: callID}))
}
