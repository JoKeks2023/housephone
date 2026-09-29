package signaling

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// session is one authenticated device connection. It implements
// calls.DeviceConn.
type session struct {
	deviceID string
	conn     *websocket.Conn
	out      chan protocol.Envelope

	mu      sync.Mutex
	closed  bool
	closeCh chan struct{}
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
	srv.applyDeviceUpdate(dev.ID, &hello.PushToken, &hello.PushEnvironment, nil)

	sess := &session{deviceID: dev.ID, conn: conn, out: make(chan protocol.Envelope, sendQueue), closeCh: make(chan struct{})}
	srv.mu.Lock()
	old := srv.sessions[dev.ID]
	srv.sessions[dev.ID] = sess
	srv.mu.Unlock()
	if old != nil {
		log.Info("replacing previous connection")
		old.close(websocket.StatusCode(protocol.CloseReplaced), "replaced")
	}
	log.Info("device connected", "ip", ip, "app", hello.AppVersion)

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
		env, err := readEnvelope(ctx, sess.conn, 0)
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
			srv.applyDeviceUpdate(sess.deviceID, u.PushToken, u.PushEnvironment, u.DeviceName)
		case protocol.TypeDeviceUnpair:
			if err := srv.cfg.Devices.Remove(sess.deviceID); err != nil && !errors.Is(err, store.ErrDeviceNotFound) {
				srv.log.Error("unpairing device failed", "device", sess.deviceID, "error", err)
				sess.Send(errorEnvelope(protocol.ErrorInternal, "Gerät konnte nicht entfernt werden"))
				continue
			}
			srv.log.Info("device unpaired itself", "device", sess.deviceID)
			sess.close(websocket.StatusNormalClosure, "unpaired")
			return
		case protocol.TypeHello, protocol.TypePair:
			sess.Send(errorEnvelope(protocol.ErrorBadRequest, env.Type+" is not allowed here"))
		default:
			srv.cfg.Hub.HandleDeviceMessage(sess, env)
		}
	}
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

// applyDeviceUpdate stores valid fields; invalid values are ignored.
func (srv *Server) applyDeviceUpdate(id string, token, environment, name *string) {
	_, err := srv.cfg.Devices.Update(id, func(d *store.Device) {
		if token != nil && *token != "" {
			if validPushToken(*token) {
				d.PushToken = *token
			} else {
				srv.log.Warn("ignoring invalid push token", "device", id)
			}
		}
		if environment != nil && *environment != "" && validEnvironment(*environment) {
			d.PushEnvironment = *environment
		}
		if name != nil && *name != "" {
			d.Name = truncateRunes(*name, maxDeviceNameRunes)
		}
		d.LastSeen = srv.cfg.Now().UTC()
	})
	if err != nil {
		srv.log.Warn("updating device failed", "device", id, "error", err)
	}
}
