package calls

import (
	"context"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

func (c *call) peerCodecs() []codec.Codec {
	if c.dir == directionIncoming {
		return []codec.Codec{c.codec}
	}
	return codec.Preferred
}

// onAttach handles call.attach (and implicit attach on accept).
func (c *call) onAttach(conn DeviceConn) {
	id := conn.DeviceID()
	if c.dir == directionOutgoing {
		c.onReattachOutgoing(conn)
		return
	}
	switch c.phase {
	case phaseRinging:
		c.participants[id] = true
		delete(c.notified, id)
		delete(c.informed, id)
		l := c.legs[id]
		if l == nil {
			ws := c.m.usesWebSocketAudio(id)
			if ws && !c.sipIn.Offers(codec.PCMA) {
				// The watch only speaks PCMA and the bridge never transcodes.
				c.log.Info("websocket-pcma device cannot take this call: no PCMA offered", "device", id)
				c.sendEnded(id, conn, protocol.EndReasonFailed, 488)
				c.checkAllDeclined()
				return
			}
			l = &leg{deviceID: id, ws: ws}
			c.legs[id] = l
		}
		l.conn = conn
		conn.Send(c.incomingEnvelope())
		c.reoffer(l)
	case phaseAnswering, phaseConnected:
		if id != c.acceptedBy {
			c.sendEnded(id, conn, protocol.EndReasonAnsweredElsewhere, 0)
			return
		}
		l := c.legs[id]
		if l == nil {
			l = &leg{deviceID: id}
			c.legs[id] = l
		}
		c.log.Info("active device re-attached", "device", id)
		l.conn = conn
		l.iceRestarts = 0
		c.stopReattachTimer()
		c.reoffer(l)
	}
}

// reoffer answers a (repeated) call.attach. call.attach is idempotent and
// never creates a second PeerConnection for the same device:
//   - an offer still being created is delivered to the current connection,
//   - an offer that was sent but not answered yet is sent again unchanged
//     (same ICE credentials, so either answer matches),
//   - an answered PeerConnection is re-offered with an ICE restart (same
//     DTLS fingerprint).
//
// websocket-pcma legs get call.media instead (v1.1).
func (c *call) reoffer(l *leg) {
	switch {
	case l.ws:
		c.attachWSMedia(l)
	case l.offerInFlight:
	case l.peer != nil && !l.answered && l.lastOffer != "":
		c.sendOffer(l, l.lastOffer)
	default:
		c.offer(l, c.peerCodecs(), l.peer != nil)
	}
}

// onAccept handles call.accept for incoming calls.
func (c *call) onAccept(conn DeviceConn) {
	id := conn.DeviceID()
	if c.dir != directionIncoming {
		sendError(conn, protocol.ErrorBadRequest, "call.accept is only valid for incoming calls", c.id)
		return
	}
	if c.phase != phaseRinging {
		if id != c.acceptedBy {
			c.sendEnded(id, conn, protocol.EndReasonAnsweredElsewhere, 0)
		}
		return
	}
	if l := c.legs[id]; l == nil || l.conn != conn {
		// Accepted before attaching (race in the app): attach implicitly.
		c.onAttach(conn)
	}
	accepted := c.legs[id]
	if accepted == nil {
		// The implicit attach refused this device (e.g. no PCMA for the watch).
		return
	}
	// The SIP answer uses the codec of the accepting device (v1.1): the
	// offered codec for WebRTC devices, PCMA for websocket-pcma devices.
	if accepted.ws {
		c.codec = codec.PCMA
	}
	c.acceptedBy = id
	c.phase = phaseAnswering
	c.log.Info("accepted", "device", id, "codec", c.codec)

	for otherID, other := range c.legs {
		if otherID == id {
			continue
		}
		c.sendEnded(otherID, other.conn, protocol.EndReasonAnsweredElsewhere, 0)
		c.dropLeg(other)
	}
	for otherID, informed := range c.informed {
		c.sendEnded(otherID, informed, protocol.EndReasonAnsweredElsewhere, 0)
	}
	c.informed = map[string]DeviceConn{}
	c.notified = map[string]bool{}

	sip, answerCodec := c.sipIn, c.codec
	answerCtx, cancel := context.WithTimeout(context.Background(), c.m.opts.AnswerTimeout)
	c.answerDone = make(chan struct{})
	answerDone := c.answerDone
	go func() {
		defer close(answerDone)
		defer cancel()
		media, err := sip.Answer(answerCtx, answerCodec)
		c.do(func() { c.onAnswered(media, err) })
	}()
}

func (c *call) onAnswered(media SIPMedia, err error) {
	if c.phase == phaseEnded {
		return
	}
	accepted := c.legs[c.acceptedBy]
	if err != nil {
		c.log.Error("answering SIP call failed", "error", err)
		c.finish(protocol.EndReasonFailed, 0)
		return
	}
	c.sipMedia = media
	c.phase = phaseConnected
	c.lastState = protocol.CallStateConnected
	if c.hangupRequested {
		c.hangupSIP()
		c.finish(protocol.EndReasonLocalHangup, 0)
		return
	}
	if accepted != nil {
		c.sendState(accepted.conn, protocol.CallStateConnected)
	}
	c.markConnectedMedia()
	c.ensureRelay()
}

// onHangup handles call.hangup from a device.
func (c *call) onHangup(conn DeviceConn, reason string) {
	if c.dir == directionOutgoing {
		c.onHangupOutgoing(conn)
		return
	}
	id := conn.DeviceID()
	switch c.phase {
	case phaseRinging:
		c.sendEnded(id, conn, protocol.EndReasonLocalHangup, 0)
		if l := c.legs[id]; l != nil {
			c.dropLeg(l)
		}
		delete(c.notified, id)
		delete(c.informed, id)
		c.declined = true
		c.log.Info("device declined", "device", id, "reason", reason)
		c.checkAllDeclined()
	case phaseAnswering:
		c.sendEnded(id, conn, protocol.EndReasonLocalHangup, 0)
		if id == c.acceptedBy {
			c.hangupRequested = true
		}
	case phaseConnected:
		c.sendEnded(id, conn, protocol.EndReasonLocalHangup, 0)
		if id == c.acceptedBy {
			c.hangupSIP()
			c.finish(protocol.EndReasonLocalHangup, 0)
		}
	}
}

// checkAllDeclined rejects the INVITE once no device can answer anymore:
// 486 if a device declined, 480 if devices were just unreachable.
func (c *call) checkAllDeclined() {
	if c.phase != phaseRinging || len(c.legs) > 0 || len(c.notified) > 0 {
		return
	}
	status, text, reason := 480, "Temporarily Unavailable", protocol.EndReasonFailed
	if c.declined {
		status, text, reason = 486, "Busy Here", protocol.EndReasonDeclinedElsewhere
	}
	c.log.Info("no device left to answer, rejecting", "status", status)
	if err := c.sipIn.Reject(status, text); err != nil {
		c.log.Warn("rejecting INVITE failed", "status", status, "error", err)
	}
	c.finish(reason, status)
}

// onPushFailed forgets a device that could not be woken.
func (c *call) onPushFailed(deviceID string) {
	if c.phase != phaseRinging {
		return
	}
	if _, attached := c.legs[deviceID]; attached {
		return
	}
	if _, online := c.informed[deviceID]; online {
		return
	}
	delete(c.notified, deviceID)
	c.checkAllDeclined()
}

// onSIPEnded handles CANCEL/BYE/timeouts from the FRITZ!Box side.
func (c *call) onSIPEnded() {
	if c.phase == phaseEnded {
		return
	}
	var r SIPEndReason
	switch {
	case c.sipIn != nil:
		r = c.sipIn.EndReason()
	default:
		r = SIPEndRemoteHangup
	}
	reason := sipEndReason(r, c.phase == phaseConnected)
	c.log.Info("SIP side ended", "reason", reason)
	c.finish(reason, 0)
}

// waitAnswer blocks until an in-flight SIP answer returned, so the SIP
// dialog is never released while 200 OK is still being retransmitted.
func (c *call) waitAnswer() {
	if c.answerDone != nil {
		<-c.answerDone
	}
}
