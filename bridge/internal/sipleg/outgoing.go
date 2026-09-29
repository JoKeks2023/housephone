package sipleg

import (
	"context"
	"errors"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
)

// Dial implements calls.SIPLeg. The INVITE offers only codec c (plus
// telephone-event), so both legs use the same codec.
func (l *Leg) Dial(ctx context.Context, number string, c codec.Codec, ev calls.DialEvents) (calls.OutgoingSIPCall, calls.SIPMedia, error) {
	recipient := sip.Uri{Scheme: "sip", User: number, Host: l.cfg.Registrar, Port: l.cfg.Port}
	d, err := l.dg.NewDialog(recipient, diago.NewDialogOptions{})
	if err != nil {
		return nil, nil, err
	}
	d.InviteRequest.AppendHeader(&sip.FromHeader{
		Address: sip.Uri{Scheme: "sip", User: l.cfg.Username, Host: l.cfg.Registrar},
		Params:  sip.NewParams(),
	})

	var media *sipMedia
	onResponse := func(res *sip.Response) error {
		if res.StatusCode == sip.StatusRinging && len(res.Body()) == 0 && ev.OnRinging != nil {
			ev.OnRinging()
		}
		return nil
	}
	med, err := d.Invite(ctx, diago.InviteClientOptions{
		Originator:       codecOriginator(c),
		Username:         l.cfg.Username,
		Password:         l.cfg.Password,
		EarlyMediaDetect: true,
		OnResponse:       onResponse,
	})
	if errors.Is(err, diago.ErrClientEarlyMedia) {
		media = newSIPMedia(med)
		if ev.OnEarlyMedia != nil {
			ev.OnEarlyMedia(media)
		}
		err = d.WaitAnswer(ctx, med, sipgo.AnswerOptions{
			Username:   l.cfg.Username,
			Password:   l.cfg.Password,
			OnResponse: onResponse,
		})
	}
	if err != nil {
		_ = d.Close()
		return nil, nil, dialError(err)
	}
	if err := d.Ack(ctx); err != nil {
		_ = d.Close()
		return nil, nil, err
	}
	if media == nil {
		media = newSIPMedia(med)
	}
	return &outgoingCall{d: d}, media, nil
}

func dialError(err error) error {
	var resErr *sipgo.ErrDialogResponse
	if errors.As(err, &resErr) && resErr.Res != nil {
		return &calls.DialError{Status: resErr.Res.StatusCode, Reason: resErr.Res.Reason}
	}
	var resErrValue sipgo.ErrDialogResponse
	if errors.As(err, &resErrValue) && resErrValue.Res != nil {
		return &calls.DialError{Status: resErrValue.Res.StatusCode, Reason: resErrValue.Res.Reason}
	}
	return err
}

type outgoingCall struct {
	d *diago.DialogClientSession
}

func (o *outgoingCall) Hangup(ctx context.Context) error { return o.d.Hangup(ctx) }
func (o *outgoingCall) Done() <-chan struct{}            { return o.d.Context().Done() }

// codecOriginator narrows diago's offer: diago filters its codecs by the
// originator's SDP (meant for bridging two SIP legs).
func codecOriginator(c codec.Codec) diago.DialogSession {
	req := sip.NewRequest(sip.INVITE, sip.Uri{Scheme: "sip", Host: "housephone.invalid"})
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.SetBody(offerSDP(c))
	return &originator{dialog: &sipgo.Dialog{InviteRequest: req}}
}

type originator struct {
	dialog *sipgo.Dialog
}

func (o *originator) Id() string                       { return "housephone-device" }
func (o *originator) Context() context.Context         { return context.Background() }
func (o *originator) Hangup(ctx context.Context) error { return nil }
func (o *originator) DialogSIP() *sipgo.Dialog         { return o.dialog }
func (o *originator) Close() error                     { return nil }
func (o *originator) Do(ctx context.Context, req *sip.Request) (*sip.Response, error) {
	return nil, errors.New("originator is not a real dialog")
}
