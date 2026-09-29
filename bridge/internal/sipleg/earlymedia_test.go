//go:build !race

// This test is excluded from -race runs: diago v0.40.0 updates the running
// media session on 200 OK after early media (MediaSession.SetRemoteAddr)
// while its RTCP writer reads the remote address — an upstream data race
// (media_session.go:331 vs rtp_session.go:538). CI runs it without -race.

package sipleg_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
)

func TestOutgoingCallWithRingingEarlyMediaAndAnswer(t *testing.T) {
	e := setup(t, nil)
	e.box.SetOnInvite(func(d *diago.DialogServerSession) {
		_ = d.Ringing()
		early, err := d.ProgressMedia(diago.ProgressMediaOptions{})
		if err != nil {
			return
		}
		rtpSess := early.RTPSession()
		for i := range 5 {
			_ = rtpSess.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: uint16(i), Timestamp: uint32(i * 160), SSRC: 0xE1}, Payload: []byte("ringback")})
			time.Sleep(20 * time.Millisecond)
		}
		if err := d.AnswerEarlyMedia(early, diago.AnswerOptions{}); err != nil {
			return
		}
		<-d.Context().Done()
	})

	var rang, early bool
	earlyMedia := make(chan calls.SIPMedia, 1)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, sipMedia, err := e.leg.Dial(ctx, "+4930999", codec.PCMA, calls.DialEvents{
		OnRinging: func() { rang = true },
		OnEarlyMedia: func(m calls.SIPMedia) {
			early = true
			earlyMedia <- m
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rang || !early {
		t.Fatalf("ringing=%v early=%v", rang, early)
	}
	if m := <-earlyMedia; m != sipMedia {
		t.Fatal("answered media must be the early media object")
	}
	invite := <-e.box.Invites
	if invite.Recipient.User != "+4930999" || invite.From().Address.User != "620" {
		t.Fatalf("INVITE to %s from %s", invite.Recipient.User, invite.From().Address.User)
	}
	offer := string(invite.Body())
	if !strings.Contains(offer, "RTP/AVP 8 101") || strings.Contains(offer, "G722") {
		t.Fatalf("offer must contain only PCMA + telephone-event:\n%s", offer)
	}
	readRTP(t, sipMedia.ReadRTP, func(p *rtp.Packet) bool { return bytes.Equal(p.Payload, []byte("ringback")) })

	if err := out.Hangup(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-out.Done():
	case <-time.After(timeout):
		t.Fatal("dialog not ended after hangup")
	}
}
