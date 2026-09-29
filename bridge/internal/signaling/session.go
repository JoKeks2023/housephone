package signaling

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// audioQueue bounds binary audio frames waiting to be written (500 ms).
const audioQueue = 25

// session is one authenticated device connection. It implements
// calls.DeviceConn.
type session struct {
	deviceID string
	conn     *websocket.Conn
	out      chan protocol.Envelope
	audio    chan []byte

	mu      sync.Mutex
	closed  bool
	closeCh chan struct{}

	sinkMu sync.Mutex
	sink   calls.AudioSink
}

func newSession(deviceID string, conn *websocket.Conn) *session {
	return &session{
		deviceID: deviceID,
		conn:     conn,
		out:      make(chan protocol.Envelope, sendQueue),
		audio:    make(chan []byte, audioQueue),
		closeCh:  make(chan struct{}),
	}
}

func (s *session) DeviceID() string { return s.deviceID }

// Send queues a message. A device that cannot keep up is disconnected.
func (s *session) Send(env protocol.Envelope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.out <- env:
	default:
		go s.close(websocket.StatusPolicyViolation, "send queue full")
	}
}

// SendAudio queues a binary audio frame (v1.1). Unlike Send it drops the
// frame instead of the connection when the device falls behind: late audio
// is worthless.
func (s *session) SendAudio(frame []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.audio <- frame:
	default:
	}
}

// SetAudioSink routes binary frames from the device to sink (v1.1).
func (s *session) SetAudioSink(sink calls.AudioSink) {
	s.sinkMu.Lock()
	s.sink = sink
	s.sinkMu.Unlock()
}

// RemoveAudioSink clears the sink if it is still sink.
func (s *session) RemoveAudioSink(sink calls.AudioSink) {
	s.sinkMu.Lock()
	if s.sink == sink {
		s.sink = nil
	}
	s.sinkMu.Unlock()
}

func (s *session) audioSink() calls.AudioSink {
	s.sinkMu.Lock()
	defer s.sinkMu.Unlock()
	return s.sink
}

// close marks the session closed and starts the close handshake without
// waiting for an unresponsive peer (runDevice force-closes on exit).
func (s *session) close(code websocket.StatusCode, reason string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.closeCh)
	s.mu.Unlock()
	go func() { _ = s.conn.Close(code, reason) }()
}

// runDevice serves an authenticated device until the connection ends.
func (srv *Server) runDevice(ctx context.Context, conn *websocket.Conn, dev store.Device, ip string) {
	defer conn.CloseNow()
	log := srv.log.With("device", dev.ID, "name", dev.Name)

	env, err := readEnvelope(ctx, conn, srv.cfg.FirstMessageWait)
	if err != nil || env.Type != protocol.TypeHello {
		_ = writeEnvelope(ctx, conn, errorEnvelope(protocol.ErrorBadRequest, "hello expected"))
		_ = conn.Close(websocket.StatusPolicyViolation, "hello expected")
		return
	}
	var hello protocol.Hello
	if err := env.Decode(&hello); err != nil {
		_ = writeEnvelope(ctx, conn, errorEnvelope(protocol.ErrorBadRequest, err.Error()))
		return
	}
	// hello states the device's current capabilities and topic: absent
	// fields mean the defaults (["webrtc"], configured topic).
	capabilities := hello.MediaCapabilities
	changes := deviceChanges{
		pushToken:         &hello.PushToken,
		pushEnvironment:   &hello.PushEnvironment,
		mediaCapabilities: &capabilities,
		pushTopic:         &hello.PushTopic,
	}
	helloErr := srv.validateDeviceChanges(changes)
	if helloErr != nil {
		// Keep the connection, but ignore the invalid topic.
		changes.pushTopic = nil
	}
	srv.applyDeviceChanges(dev.ID, changes)

	sess := newSession(dev.ID, conn)
	srv.mu.Lock()
	old := srv.sessions[dev.ID]
	srv.sessions[dev.ID] = sess
	srv.mu.Unlock()
	if old != nil {
		log.Info("replacing previous connection")
		old.close(websocket.StatusCode(protocol.CloseReplaced), "replaced")
	}
	log.Info("device connected", "ip", ip, "app", hello.AppVersion, "platform", hello.Platform, "media", hello.MediaCapabilities)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Once the session is closed, give the close handshake a moment and then
	// tear the connection down even if the peer does not answer.
	go func() {
		select {
		case <-sess.closeCh:
		case <-ctx.Done():
			return
		}
		select {
		case <-time.After(closeGrace):
			cancel()
		case <-ctx.Done():
		}
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		srv.writeLoop(ctx, sess)
	}()
	go func() {
		defer wg.Done()
		srv.pingLoop(ctx, sess)
	}()

	sess.Send(protocol.MustEnvelope(protocol.TypeWelcome, protocol.Welcome{
		BridgeID:      srv.cfg.BridgeID,
		BridgeName:    srv.cfg.BridgeName,
		BridgeVersion: srv.cfg.BridgeVersion,
		SIPRegistered: srv.cfg.Hub.SIPRegistered(),
	}))
	if helloErr != nil {
		sess.Send(errorEnvelope(protocol.ErrorBadRequest, helloErr.Error()))
	}
	srv.cfg.Hub.DeviceConnected(sess)

	srv.readLoop(ctx, sess)

	sess.close(websocket.StatusNormalClosure, "")
	cancel()
	wg.Wait()
	srv.cfg.Hub.DeviceDisconnected(sess)
	srv.mu.Lock()
	if srv.sessions[dev.ID] == sess {
		delete(srv.sessions, dev.ID)
	}
	srv.mu.Unlock()
	if _, err := srv.cfg.Devices.Update(dev.ID, func(d *store.Device) { d.LastSeen = srv.cfg.Now().UTC() }); err != nil && !errors.Is(err, store.ErrDeviceNotFound) {
		log.Warn("updating lastSeen failed", "error", err)
	}
	log.Info("device disconnected")
}

func (srv *Server) readLoop(ctx context.Context, sess *session) {
	for {
		typ, data, err := sess.conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			// websocket-pcma audio (v1.1); ignored without an active call.
			if sink := sess.audioSink(); sink != nil {
				sink.DeviceAudio(data)
			}
			continue
		}
		env, err := protocol.ParseEnvelope(data)
		if err != nil {
			return
		}
		switch env.Type {
		case protocol.TypeDeviceUpdate:
			var u protocol.DeviceUpdate
			if err := env.Decode(&u); err != nil {
				sess.Send(errorEnvelope(protocol.ErrorBadRequest, err.Error()))
				continue
			}
			changes := changesFromUpdate(u)
			if err := srv.validateDeviceChanges(changes); err != nil {
				sess.Send(errorEnvelope(protocol.ErrorBadRequest, err.Error()))
				continue
			}
			srv.applyDeviceChanges(sess.deviceID, changes)
		case protocol.TypeDeviceUnpair:
			if err := srv.cfg.Devices.Remove(sess.deviceID); err != nil && !errors.Is(err, store.ErrDeviceNotFound) {
				srv.log.Error("unpairing device failed", "device", sess.deviceID, "error", err)
				sess.Send(errorEnvelope(protocol.ErrorInternal, "Gerät konnte nicht entfernt werden"))
				continue
			}
			srv.log.Info("device unpaired itself", "device", sess.deviceID)
			sess.close(websocket.StatusNormalClosure, "unpaired")
			return
		case protocol.TypePairCompanionRequest:
			srv.pairCompanion(sess, env)
		case protocol.TypeHello, protocol.TypePair:
			sess.Send(errorEnvelope(protocol.ErrorBadRequest, env.Type+" is not allowed here"))
		default:
			srv.cfg.Hub.HandleDeviceMessage(sess, env)
		}
	}
}

// pairCompanion creates a pairing code on behalf of a paired device, e.g.
// the iPhone pairing its watch (v1.1).
func (srv *Server) pairCompanion(sess *session, env protocol.Envelope) {
	var req protocol.PairCompanionRequest
	if err := env.Decode(&req); err != nil || !validPlatform(req.Platform) {
		sess.Send(errorEnvelope(protocol.ErrorBadRequest, "deviceName and platform (ios|watchos) are required"))
		return
	}
	pc, err := srv.cfg.Pairing.Create(truncateRunes(strings.TrimSpace(req.DeviceName), maxDeviceNameRunes), srv.cfg.Now())
	if err != nil {
		srv.log.Error("creating companion pairing code failed", "device", sess.deviceID, "error", err)
		sess.Send(errorEnvelope(protocol.ErrorInternal, "Kopplungscode konnte nicht erzeugt werden"))
		return
	}
	srv.log.Info("companion pairing code created", "device", sess.deviceID, "for", pc.Name, "platform", req.Platform)
	sess.Send(protocol.MustEnvelope(protocol.TypePairCompanion, protocol.PairCompanion{
		Code:      pc.Code,
		URL:       srv.cfg.PublicURL,
		ExpiresAt: protocol.Timestamp(pc.ExpiresAt),
	}))
}

func (srv *Server) writeLoop(ctx context.Context, sess *session) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-sess.closeCh:
			return
		case env := <-sess.out:
			if err := writeEnvelope(ctx, sess.conn, env); err != nil {
				sess.close(websocket.StatusInternalError, "write failed")
				return
			}
		case frame := <-sess.audio:
			if err := writeBinary(ctx, sess.conn, frame); err != nil {
				sess.close(websocket.StatusInternalError, "write failed")
				return
			}
		}
	}
}

func (srv *Server) pingLoop(ctx context.Context, sess *session) {
	ticker := time.NewTicker(srv.cfg.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sess.closeCh:
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, srv.cfg.PingInterval)
			err := sess.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				sess.close(websocket.StatusGoingAway, "ping timeout")
				return
			}
		}
	}
}

// deviceChanges are optional device fields from hello, device.update or
// PUT /v1/device. nil pointers leave the stored value unchanged.
type deviceChanges struct {
	pushToken         *string
	pushEnvironment   *string
	name              *string
	mediaCapabilities *[]string
	pushTopic         *string
}

func changesFromUpdate(u protocol.DeviceUpdate) deviceChanges {
	ch := deviceChanges{pushToken: u.PushToken, pushEnvironment: u.PushEnvironment, name: u.DeviceName, pushTopic: u.PushTopic}
	if u.MediaCapabilities != nil {
		ch.mediaCapabilities = &u.MediaCapabilities
	}
	return ch
}

// validateDeviceChanges rejects push topics outside the app's bundle
// (v1.1): the topic must be the configured one or share its bundle prefix,
// e.g. com.jorisconrad.housephone.watchkitapp.voip.
func (srv *Server) validateDeviceChanges(ch deviceChanges) error {
	if ch.pushTopic == nil || *ch.pushTopic == "" {
		return nil
	}
	if !srv.validPushTopic(*ch.pushTopic) {
		return errors.New("pushTopic must belong to the app's bundle and end in .voip")
	}
	return nil
}

func (srv *Server) validPushTopic(topic string) bool {
	if len(topic) > 200 || !strings.HasSuffix(topic, ".voip") || strings.ContainsAny(topic, " /\t\r\n") {
		return false
	}
	configured := srv.cfg.PushTopic
	if configured == "" {
		return true
	}
	if topic == configured {
		return true
	}
	prefix := strings.TrimSuffix(configured, ".voip")
	return strings.HasPrefix(topic, prefix+".")
}

// applyDeviceChanges stores valid fields; invalid values are ignored.
func (srv *Server) applyDeviceChanges(id string, ch deviceChanges) {
	_, err := srv.cfg.Devices.Update(id, func(d *store.Device) {
		if ch.pushToken != nil && *ch.pushToken != "" {
			if validPushToken(*ch.pushToken) {
				d.PushToken = *ch.pushToken
			} else {
				srv.log.Warn("ignoring invalid push token", "device", id)
			}
		}
		if ch.pushEnvironment != nil && *ch.pushEnvironment != "" && validEnvironment(*ch.pushEnvironment) {
			d.PushEnvironment = *ch.pushEnvironment
		}
		if ch.name != nil && *ch.name != "" {
			d.Name = truncateRunes(*ch.name, maxDeviceNameRunes)
		}
		if ch.mediaCapabilities != nil {
			d.MediaCapabilities = knownCapabilities(*ch.mediaCapabilities)
		}
		if ch.pushTopic != nil && (*ch.pushTopic == "" || srv.validPushTopic(*ch.pushTopic)) {
			d.PushTopic = *ch.pushTopic
		}
		d.LastSeen = srv.cfg.Now().UTC()
	})
	if err != nil {
		srv.log.Warn("updating device failed", "device", id, "error", err)
	}
}

// knownCapabilities keeps the media capabilities this bridge understands, in
// order and without duplicates. An empty result means the default (WebRTC).
func knownCapabilities(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range in {
		if (c == protocol.MediaWebRTC || c == protocol.MediaWebSocketPCMA) && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}
