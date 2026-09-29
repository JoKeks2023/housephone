package calls

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

type direction int

const (
	directionIncoming direction = iota
	directionOutgoing
)

func (d direction) String() string {
	if d == directionIncoming {
		return "incoming"
	}
	return "outgoing"
}

type phase int

const (
	// phaseRinging: incoming waits for accept; outgoing offers and dials.
	phaseRinging phase = iota
	// phaseAnswering: incoming SIP 200 OK sent, waiting for ACK.
	phaseAnswering
	phaseConnected
	phaseEnded
)

// maxICERestarts bounds bridge-initiated ICE restarts per leg.
const maxICERestarts = 3

// leg is one device's participation in a call.
type leg struct {
	deviceID string
	// conn is nil while the device is detached (WebSocket lost).
	conn DeviceConn
	// ws: the device uses the websocket-pcma media path (v1.1); peer is then
	// a *wsPeer and there is no offer/answer.
	ws   bool
	peer Peer
	// peerGen identifies the current peer for state callbacks.
	peerGen int
	// offerGen identifies the latest offer; stale offers are dropped.
	offerGen      int
	offerInFlight bool
	lastOffer     string
	answered      bool
	iceRestarts   int
}

// call is an actor: all fields below ops are only touched by run().
type call struct {
	m         *Manager
	id        string
	dir       direction
	log       *slog.Logger
	startedAt time.Time

	ops  chan func()
	done chan struct{}

	caller     string
	callerName string
	number     string
	codec      codec.Codec

	phase phase
	legs  map[string]*leg
	// notified: devices pushed or informed that have not attached or declined.
	notified map[string]bool
	// informed: connections that received call.incoming without attaching.
	informed map[string]DeviceConn
	// endedSent: devices that already received call.ended.
	endedSent  map[string]bool
	acceptedBy string

	sipIn    IncomingSIPCall
	sipOut   OutgoingSIPCall
	sipMedia SIPMedia
	relay    *relay

	dialCancel      context.CancelFunc
	dialing         bool
	hangupRequested bool
	declined        bool
	lastState       string
	answerDone      chan struct{}
	reattachTimer   *time.Timer
	peerSeq         int

	endReason  string
	endSIPCode int
	finished   bool
}

func newCall(m *Manager, id string, dir direction) *call {
	return &call{
		m:         m,
		id:        id,
		dir:       dir,
		log:       m.log.With("call", id, "direction", dir.String()),
		startedAt: protocol.Timestamp(m.opts.Now()),
		ops:       make(chan func(), 64),
		done:      make(chan struct{}),
		legs:      map[string]*leg{},
		notified:  map[string]bool{},
		informed:  map[string]DeviceConn{},
		endedSent: map[string]bool{},
	}
}

// do schedules op on the actor. It returns false if the call already ended.
func (c *call) do(op func()) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.ops <- op:
		return true
	case <-c.done:
		return false
	}
}

// run processes ops until the call finishes.
func (c *call) run() {
	defer close(c.done)
	for !c.finished {
		op := <-c.ops
		op()
	}
	c.cleanup()
}

func (c *call) cleanup() {
	if c.reattachTimer != nil {
		c.reattachTimer.Stop()
	}
	if c.dialCancel != nil {
		c.dialCancel()
	}
	if c.relay != nil {
		c.relay.stop()
	}
	for _, l := range c.legs {
		if l.peer != nil {
			_ = l.peer.Close()
		}
	}
	c.log.Info("call ended", "reason", c.endReason, "sipCode", c.endSIPCode, "duration", time.Since(c.startedAt).Round(time.Second))
}

// finish ends the call and tells every remaining participant.
func (c *call) finish(reason string, sipCode int) {
	if c.finished {
		return
	}
	c.phase = phaseEnded
	c.endReason, c.endSIPCode = reason, sipCode
	for id, l := range c.legs {
		if l.conn != nil {
			c.sendEnded(id, l.conn, reason, sipCode)
		}
	}
	for id, conn := range c.informed {
		c.sendEnded(id, conn, reason, sipCode)
	}
	c.finished = true
}

func (c *call) sendEnded(deviceID string, conn DeviceConn, reason string, sipCode int) {
	if c.endedSent[deviceID] || conn == nil {
		return
	}
	c.endedSent[deviceID] = true
	conn.Send(protocol.MustEnvelope(protocol.TypeCallEnded, protocol.CallEnded{CallID: c.id, Reason: reason, SIPCode: sipCode}))
}

func (c *call) send(conn DeviceConn, msgType string, payload any) {
	if conn != nil {
		conn.Send(protocol.MustEnvelope(msgType, payload))
	}
}

func (c *call) sendState(conn DeviceConn, state string) {
	c.send(conn, protocol.TypeCallState, protocol.CallState{CallID: c.id, State: state})
}

func (c *call) incomingEnvelope() protocol.Envelope {
	return protocol.MustEnvelope(protocol.TypeCallIncoming, protocol.CallIncoming{
		CallID:     c.id,
		Caller:     c.caller,
		CallerName: c.callerName,
		StartedAt:  c.startedAt,
	})
}

// dropLeg removes a leg and closes its peer.
func (c *call) dropLeg(l *leg) {
	if l.peer != nil {
		_ = l.peer.Close()
		l.peer = nil
	}
	delete(c.legs, l.deviceID)
}

// offer creates (or reuses) the leg's peer and sends call.offer when ready.
func (c *call) offer(l *leg, codecs []codec.Codec, iceRestart bool) {
	l.offerInFlight = true
	l.offerGen++
	gen := l.offerGen
	existing := l.peer
	peerGen := l.peerGen
	if existing == nil {
		c.peerSeq++
		peerGen = c.peerSeq
	}
	go func() {
		peer := existing
		if peer == nil {
			onState := func(s PeerState) {
				c.do(func() { c.onPeerState(l, peerGen, s) })
			}
			p, err := c.m.opts.Peers.NewPeer(codecs, onState)
			if err != nil {
				c.do(func() { c.onOfferReady(l, gen, nil, 0, "", err) })
				return
			}
			peer = p
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.m.opts.OfferTimeout)
		sdp, err := peer.CreateOffer(ctx, iceRestart && existing != nil)
		cancel()
		var created Peer
		if existing == nil {
			created = peer
		}
		if !c.do(func() { c.onOfferReady(l, gen, created, peerGen, sdp, err) }) && created != nil {
			_ = created.Close()
		}
	}()
}

func (c *call) onOfferReady(l *leg, gen int, created Peer, peerGen int, sdp string, err error) {
	if l.offerGen == gen {
		l.offerInFlight = false
	}
	stale := c.phase == phaseEnded || c.legs[l.deviceID] != l || l.offerGen != gen
	if stale {
		if created != nil {
			_ = created.Close()
		}
		return
	}
	if created != nil {
		l.peer = created
		l.peerGen = peerGen
	}
	if err != nil {
		c.log.Error("creating offer failed", "device", l.deviceID, "error", err)
		sendError(l.conn, protocol.ErrorInternal, "creating WebRTC offer failed", c.id)
		c.legFailed(l)
		return
	}
	l.answered = false
	l.lastOffer = sdp
	c.sendOffer(l, sdp)
}

// sendOffer sends call.offer and, for a re-attached device, the current state.
func (c *call) sendOffer(l *leg, sdp string) {
	c.send(l.conn, protocol.TypeCallOffer, protocol.CallOffer{CallID: c.id, SDP: sdp, ICEServers: c.m.opts.ICEServers})
	if c.lastState != "" && (c.phase == phaseConnected || c.dir == directionOutgoing) {
		c.sendState(l.conn, c.lastState)
	}
}

// legFailed handles a leg that cannot continue (offer/answer error).
func (c *call) legFailed(l *leg) {
	isOwner := c.dir == directionOutgoing || l.deviceID == c.acceptedBy
	if isOwner {
		c.hangupSIP()
		c.finish(protocol.EndReasonFailed, 0)
		return
	}
	c.sendEnded(l.deviceID, l.conn, protocol.EndReasonFailed, 0)
	c.dropLeg(l)
	c.checkAllDeclined()
}

func (c *call) onAnswer(conn DeviceConn, sdp string) {
	l := c.legs[conn.DeviceID()]
	if l != nil && l.ws {
		sendError(conn, protocol.ErrorBadRequest, "websocket-pcma devices do not answer offers", c.id)
		return
	}
	if l == nil || l.conn != conn || l.peer == nil {
		sendError(conn, protocol.ErrorBadRequest, "no offer pending for this device", c.id)
		return
	}
	if l.answered {
		// Duplicate answer to a re-sent offer (repeated call.attach).
		c.log.Debug("ignoring answer for already answered offer", "device", l.deviceID)
		return
	}
	if err := l.peer.SetAnswer(sdp); err != nil {
		c.log.Warn("invalid answer", "device", l.deviceID, "error", err)
		sendError(conn, protocol.ErrorBadRequest, "invalid SDP answer", c.id)
		c.legFailed(l)
		return
	}
	l.answered = true
	switch c.dir {
	case directionIncoming:
		if l.deviceID == c.acceptedBy {
			c.ensureRelay()
		}
	case directionOutgoing:
		if !c.dialing && c.phase == phaseRinging {
			codecChoice, err := l.peer.Codec()
			if err != nil {
				c.log.Warn("no usable codec in answer", "error", err)
				sendError(conn, protocol.ErrorBadRequest, "answer contains no supported codec", c.id)
				c.finish(protocol.EndReasonFailed, 0)
				return
			}
			c.codec = codecChoice
			c.startDial()
		}
		c.ensureRelay()
	}
}

// ensureRelay starts forwarding media once SIP media and an answered peer of
// the active device exist. A changed peer restarts the relay.
func (c *call) ensureRelay() {
	if c.sipMedia == nil {
		return
	}
	owner := c.acceptedBy
	if c.dir == directionOutgoing {
		for id := range c.legs {
			owner = id
		}
	}
	l := c.legs[owner]
	if l == nil || l.peer == nil || !l.answered {
		return
	}
	if c.relay != nil {
		if c.relay.peer == l.peer && c.relay.sip == c.sipMedia {
			return
		}
		c.relay.stop()
	}
	c.relay = startRelay(c.sipMedia, l.peer, c.codec, c.log)
}

// attachWSMedia sets up (or re-binds after re-attach) the websocket-pcma
// media of a leg and sends call.media instead of an offer (v1.1).
func (c *call) attachWSMedia(l *leg) {
	p, _ := l.peer.(*wsPeer)
	if p == nil {
		p = newWSPeer(c.m.opts.AudioFrameInterval)
		l.peer = p
	}
	l.answered = true
	p.bind(l.conn)
	c.send(l.conn, protocol.TypeCallMedia, protocol.NewCallMedia(c.id))
	if c.lastState != "" && (c.phase == phaseConnected || c.dir == directionOutgoing) {
		c.sendState(l.conn, c.lastState)
	}
	if c.phase == phaseConnected {
		p.setLive(true)
	}
}

// markConnectedMedia lets a websocket-pcma device's audio through once the
// call is connected; before that the bridge drops it (v1.1).
func (c *call) markConnectedMedia() {
	for _, l := range c.legs {
		if p, ok := l.peer.(*wsPeer); ok && (c.dir == directionOutgoing || l.deviceID == c.acceptedBy) {
			p.setLive(true)
		}
	}
}

func (c *call) onDTMF(conn DeviceConn, digits string) {
	owner := c.dir == directionOutgoing && c.legs[conn.DeviceID()] != nil || conn.DeviceID() == c.acceptedBy
	if !owner || c.sipMedia == nil || c.phase != phaseConnected {
		sendError(conn, protocol.ErrorBadRequest, "no connected call for DTMF", c.id)
		return
	}
	media := c.sipMedia
	go func() {
		if err := media.SendDTMF(digits); err != nil {
			c.log.Warn("sending DTMF failed", "error", err)
		}
	}()
}

func (c *call) onPeerState(l *leg, peerGen int, s PeerState) {
	if c.legs[l.deviceID] != l || l.peerGen != peerGen || c.phase == phaseEnded {
		return
	}
	c.log.Debug("peer state", "device", l.deviceID, "state", s.String())
	active := c.dir == directionOutgoing || l.deviceID == c.acceptedBy
	if !active {
		if s == PeerFailed {
			c.legFailed(l)
		}
		return
	}
	switch s {
	case PeerConnected:
		if l.conn != nil {
			c.stopReattachTimer()
			l.iceRestarts = 0
		}
	case PeerFailed:
		c.startReattachTimer()
		if l.conn != nil && l.answered && l.iceRestarts < maxICERestarts {
			l.iceRestarts++
			c.log.Info("ICE failed, restarting", "device", l.deviceID, "attempt", l.iceRestarts)
			c.offer(l, nil, true)
		}
	}
}

func (c *call) onDisconnect(conn DeviceConn) {
	id := conn.DeviceID()
	if informed, ok := c.informed[id]; ok && informed == conn {
		delete(c.informed, id)
	}
	l := c.legs[id]
	if l == nil || l.conn != conn {
		return
	}
	if p, ok := l.peer.(*wsPeer); ok {
		p.bind(nil)
	}
	active := c.dir == directionOutgoing || id == c.acceptedBy
	if !active {
		// Ringing device lost its connection. Keep its PeerConnection: when
		// it attaches again it gets an ICE-restart offer on the same one.
		l.conn = nil
		return
	}
	c.log.Info("active device detached, waiting for re-attach", "device", id, "timeout", c.m.opts.ReattachTimeout)
	l.conn = nil
	c.startReattachTimer()
}

func (c *call) startReattachTimer() {
	if c.reattachTimer != nil {
		return
	}
	c.reattachTimer = time.AfterFunc(c.m.opts.ReattachTimeout, func() {
		c.do(c.onReattachTimeout)
	})
}

func (c *call) stopReattachTimer() {
	if c.reattachTimer != nil {
		c.reattachTimer.Stop()
		c.reattachTimer = nil
	}
}

func (c *call) onReattachTimeout() {
	c.reattachTimer = nil
	if c.phase == phaseEnded {
		return
	}
	for _, l := range c.legs {
		if (c.dir == directionOutgoing || l.deviceID == c.acceptedBy) && l.conn != nil {
			// Re-attached in the meantime; the peer may still recover.
			return
		}
	}
	c.log.Info("device did not re-attach in time, hanging up")
	if c.dir == directionOutgoing && c.dialing && c.sipOut == nil {
		c.hangupRequested = true
		c.dialCancel()
		c.endReason = protocol.EndReasonFailed
		return
	}
	if c.phase == phaseAnswering {
		c.hangupRequested = true
		return
	}
	c.hangupSIP()
	c.finish(protocol.EndReasonFailed, 0)
}

// hangupSIP releases the SIP side of an answered call (BYE).
func (c *call) hangupSIP() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch {
	case c.sipOut != nil:
		if err := c.sipOut.Hangup(ctx); err != nil {
			c.log.Warn("BYE failed", "error", err)
		}
	case c.sipIn != nil && (c.phase == phaseConnected):
		if err := c.sipIn.Hangup(ctx); err != nil {
			c.log.Warn("BYE failed", "error", err)
		}
	}
}

func (c *call) onShutdown() {
	if c.phase == phaseEnded {
		return
	}
	switch {
	case c.dir == directionIncoming && c.phase == phaseRinging:
		_ = c.sipIn.Reject(503, "Service Unavailable")
	case c.dir == directionOutgoing && c.dialing && c.sipOut == nil:
		c.dialCancel()
	default:
		c.hangupSIP()
	}
	c.finish(protocol.EndReasonFailed, 0)
}

// sipEndReason maps a SIP dialog end to call.ended.
func sipEndReason(r SIPEndReason, answered bool) string {
	switch r {
	case SIPEndCancelled:
		return protocol.EndReasonRemoteCancelled
	case SIPEndAnsweredElsewhere:
		return protocol.EndReasonAnsweredElsewhere
	case SIPEndFailed:
		return protocol.EndReasonFailed
	case SIPEndRemoteHangup:
		return protocol.EndReasonRemoteHangup
	}
	if answered {
		return protocol.EndReasonRemoteHangup
	}
	return protocol.EndReasonRemoteCancelled
}

// dialFailure maps an outgoing INVITE failure to call.ended.
func dialFailure(err error) (reason string, sipCode int, invalidNumber bool) {
	var de *DialError
	if !errors.As(err, &de) {
		return protocol.EndReasonFailed, 0, false
	}
	switch de.Status {
	case 486, 600:
		return protocol.EndReasonBusy, de.Status, false
	case 603:
		return protocol.EndReasonRejected, de.Status, false
	case 404, 484:
		return protocol.EndReasonFailed, de.Status, true
	}
	return protocol.EndReasonFailed, de.Status, false
}
