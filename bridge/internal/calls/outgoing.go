package calls

import (
	"context"
	"errors"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/logsafe"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// startOutgoing creates the device leg and sends the first offer. A
// websocket-pcma device gets call.media and the INVITE (PCMA only) goes out
// right away, because there is no answer to wait for (v1.1).
func (c *call) startOutgoing(conn DeviceConn) {
	l := &leg{deviceID: conn.DeviceID(), conn: conn, ws: c.m.usesWebSocketAudio(conn.DeviceID())}
	c.legs[l.deviceID] = l
	c.log.Info("outgoing call", "device", l.deviceID, "number", logsafe.Number(c.number), "websocketAudio", l.ws)
	ev := c.event(EventStarted)
	ev.DeviceID = l.deviceID
	c.m.emit(ev)
	if l.ws {
		c.codec = codec.PCMA
		c.attachWSMedia(l)
		c.startDial()
		return
	}
	c.offer(l, c.peerCodecs(), false)
	c.offerTimer = time.AfterFunc(c.m.opts.OfferAnswerTimeout, func() { c.do(c.onOfferAnswerTimeout) })
}

// onOfferAnswerTimeout ends an outgoing call whose device never answered
// the offer, so unanswered dials do not pile up (security review N3).
func (c *call) onOfferAnswerTimeout() {
	c.offerTimer = nil
	if c.phase != phaseRinging || c.dialing || c.sipOut != nil {
		return
	}
	if l := c.owner(); l != nil && l.answered {
		return
	}
	c.log.Info("offer not answered in time, ending call")
	c.finish(protocol.EndReasonFailed, 0)
}

func (c *call) owner() *leg {
	for _, l := range c.legs {
		return l
	}
	return nil
}

// startDial sends the INVITE once the device answered the offer.
func (c *call) startDial() {
	ctx, cancel := context.WithCancel(context.Background())
	c.dialCancel = cancel
	c.dialing = true
	sip := c.m.sip()
	number, chosen := c.number, c.codec
	c.log.Info("dialing", "codec", chosen)
	go func() {
		out, media, err := sip.Dial(ctx, number, chosen, DialEvents{
			OnRinging: func() { c.do(c.onRemoteRinging) },
			OnEarlyMedia: func(m SIPMedia) {
				c.do(func() { c.onEarlyMedia(m) })
			},
		})
		if !c.do(func() { c.onDialResult(out, media, err) }) && err == nil && out != nil {
			hangupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = out.Hangup(hangupCtx)
		}
	}()
}

func (c *call) onRemoteRinging() {
	if c.phase != phaseRinging || c.lastState != "" {
		return
	}
	c.lastState = protocol.CallStateRinging
	if l := c.owner(); l != nil {
		c.sendState(l.conn, protocol.CallStateRinging)
	}
}

func (c *call) onEarlyMedia(media SIPMedia) {
	if c.phase != phaseRinging || c.hangupRequested {
		return
	}
	c.sipMedia = media
	c.lastState = protocol.CallStateEarlyMedia
	if l := c.owner(); l != nil {
		c.sendState(l.conn, protocol.CallStateEarlyMedia)
	}
	c.ensureRelay()
}

func (c *call) onDialResult(out OutgoingSIPCall, media SIPMedia, err error) {
	c.dialing = false
	if c.phase == phaseEnded {
		return
	}
	l := c.owner()
	if err != nil {
		if c.hangupRequested || errors.Is(err, context.Canceled) {
			reason := c.endReason
			if reason == "" {
				reason = protocol.EndReasonLocalHangup
			}
			c.finish(reason, 0)
			return
		}
		reason, code, invalid := dialFailure(err)
		c.log.Info("dial failed", "error", err, "reason", reason)
		if invalid && l != nil {
			sendError(l.conn, protocol.ErrorInvalidNumber, "Die Nummer ist ungültig oder unbekannt", c.id)
		}
		c.finish(reason, code)
		return
	}
	c.sipOut = out
	if media != nil {
		c.sipMedia = media
	}
	if c.hangupRequested {
		c.hangupSIP()
		c.finish(protocol.EndReasonLocalHangup, 0)
		return
	}
	c.phase = phaseConnected
	c.m.emit(c.event(EventConnected))
	c.lastState = protocol.CallStateConnected
	if l != nil {
		c.sendState(l.conn, protocol.CallStateConnected)
	}
	c.markConnectedMedia()
	c.ensureRelay()
	go func() {
		select {
		case <-out.Done():
			c.do(c.onSIPEnded)
		case <-c.done:
		}
	}()
}

func (c *call) onHangupOutgoing(conn DeviceConn) {
	l := c.legs[conn.DeviceID()]
	if l == nil {
		c.sendEnded(conn.DeviceID(), conn, protocol.EndReasonNotFound, 0)
		return
	}
	c.sendEnded(l.deviceID, conn, protocol.EndReasonLocalHangup, 0)
	switch {
	case c.phase == phaseConnected:
		c.hangupSIP()
		c.finish(protocol.EndReasonLocalHangup, 0)
	case c.dialing:
		// CANCEL; onDialResult finishes the call.
		c.hangupRequested = true
		c.dialCancel()
	default:
		c.finish(protocol.EndReasonLocalHangup, 0)
	}
}

func (c *call) onReattachOutgoing(conn DeviceConn) {
	l := c.legs[conn.DeviceID()]
	if l == nil {
		c.sendEnded(conn.DeviceID(), conn, protocol.EndReasonNotFound, 0)
		return
	}
	c.log.Info("device re-attached", "device", l.deviceID)
	l.conn = conn
	l.iceRestarts = 0
	c.stopReattachTimer()
	c.reoffer(l)
}
