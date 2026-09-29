package sipleg

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo/sip"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/logsafe"
)

func (l *Leg) handleInvite(ctx context.Context, d *diago.DialogServerSession, handler func(context.Context, calls.IncomingSIPCall)) {
	callID := d.InviteRequest.CallID().Value()
	defer l.cancels.Delete(callID)

	choice, err := chooseFromOffer(d.InviteRequest.Body())
	from := logsafe.Number(d.FromUser())
	if err != nil {
		l.log.Warn("rejecting INVITE", "error", err, "from", from, "offered", choice.names)
		_ = d.DialogServerSession.Respond(sip.StatusNotAcceptableHere, "Not Acceptable Here", nil)
		return
	}
	// The offered codecs explain why a call is not HD (no G.722 offered).
	l.log.Info("incoming INVITE", "from", from, "offered", choice.names, "chosen", choice.codec)
	if err := d.Trying(); err != nil {
		l.log.Warn("sending 100 Trying failed", "error", err)
	}
	handler(ctx, &incomingCall{leg: l, d: d, choice: choice})
}

// incomingCall adapts a diago server dialog to calls.IncomingSIPCall.
type incomingCall struct {
	leg      *Leg
	d        *diago.DialogServerSession
	choice   offerChoice
	answered atomic.Bool
}

func (c *incomingCall) Caller() string {
	user := c.d.InviteRequest.From().Address.User
	if isAnonymous(user) {
		return ""
	}
	return user
}

func (c *incomingCall) CallerName() string {
	name := strings.Trim(c.d.InviteRequest.From().DisplayName, `"`)
	if isAnonymous(name) || name == c.Caller() {
		return ""
	}
	return name
}

func isAnonymous(s string) bool {
	switch strings.ToLower(s) {
	case "anonymous", "unknown", "restricted", "unavailable":
		return true
	}
	return false
}

func (c *incomingCall) Codec() codec.Codec { return c.choice.codec }

func (c *incomingCall) Offers(cd codec.Codec) bool { return c.choice.has(cd) }

func (c *incomingCall) Ringing() error { return c.d.Ringing() }

// Answer sends 200 OK with exactly codec cd and blocks until ACK.
func (c *incomingCall) Answer(ctx context.Context, cd codec.Codec) (calls.SIPMedia, error) {
	if !c.choice.has(cd) {
		return nil, fmt.Errorf("codec %s was not offered", cd)
	}
	type result struct {
		med *diago.DialogMedia
		err error
	}
	done := make(chan result, 1)
	go func() {
		med, err := c.d.Answer(diago.AnswerOptions{Codecs: c.choice.diagoCodecs(cd)})
		done <- result{med, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return nil, r.err
		}
		c.answered.Store(true)
		return newSIPMedia(r.med), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *incomingCall) Reject(status int, reason string) error {
	return c.d.DialogServerSession.Respond(status, reason, nil)
}

func (c *incomingCall) Hangup(ctx context.Context) error {
	return c.d.Hangup(ctx)
}

func (c *incomingCall) Done() <-chan struct{} { return c.d.Context().Done() }

func (c *incomingCall) EndReason() calls.SIPEndReason {
	return c.leg.endReason(c.d, c.answered.Load())
}
