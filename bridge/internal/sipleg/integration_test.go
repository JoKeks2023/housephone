package sipleg_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/fakefritz"
	"github.com/JoKeks2023/housephone/bridge/internal/sipleg"
)

const timeout = 5 * time.Second

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type env struct {
	box      *fakefritz.Box
	leg      *sipleg.Leg
	incoming chan calls.IncomingSIPCall
	// release lets the handler return (ending the dialog on the bridge side).
	release chan struct{}
}

func setup(t *testing.T, boxCodecs []media.Codec) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	box, err := fakefritz.Start(ctx, "620", "geheim", boxCodecs, quiet())
	if err != nil {
		t.Fatal(err)
	}
	port, err := fakefritz.FreeUDPPort()
	if err != nil {
		t.Fatal(err)
	}
	leg, err := sipleg.New(sipleg.Config{
		Registrar: box.Host, Port: box.Port,
		Username: "620", Password: "geheim",
		BindHost: "127.0.0.1", BindPort: port,
		RegisterExpiry: time.Minute,
		Logger:         quiet(),
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{box: box, leg: leg, incoming: make(chan calls.IncomingSIPCall, 1), release: make(chan struct{})}
	go func() {
		_ = leg.Serve(ctx, func(ctx context.Context, c calls.IncomingSIPCall) {
			e.incoming <- c
			select {
			case <-e.release:
			case <-c.Done():
			case <-ctx.Done():
			}
		})
	}()
	select {
	case <-box.Registered:
	case <-time.After(timeout):
		t.Fatal("bridge did not register")
	}
	deadline := time.Now().Add(timeout)
	for !leg.Registered() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !leg.Registered() {
		t.Fatal("leg does not report registration")
	}
	return e
}

func (e *env) nextIncoming(t *testing.T) calls.IncomingSIPCall {
	t.Helper()
	select {
	case c := <-e.incoming:
		return c
	case <-time.After(timeout):
		t.Fatal("no incoming call at the bridge")
	}
	return nil
}

func readRTP(t *testing.T, read func(buf []byte, pkt *rtp.Packet) (int, error), want func(*rtp.Packet) bool) *rtp.Packet {
	t.Helper()
	got := make(chan *rtp.Packet, 1)
	go func() {
		buf := make([]byte, 1500)
		for {
			var pkt rtp.Packet
			if _, err := read(buf, &pkt); err != nil {
				return
			}
			if want(&pkt) {
				cp := pkt
				cp.Payload = append([]byte(nil), pkt.Payload...)
				got <- &cp
				return
			}
		}
	}()
	select {
	case p := <-got:
		return p
	case <-time.After(timeout):
		t.Fatal("expected RTP packet not received")
	}
	return nil
}

func TestRegistrationUsesDigestAuth(t *testing.T) {
	e := setup(t, nil)
	if n := e.box.RegisterCount(); n < 2 {
		t.Fatalf("expected challenge + authenticated REGISTER, got %d", n)
	}
	contact, err := e.box.Contact()
	if err != nil || contact.User != "620" || contact.Host != "127.0.0.1" {
		t.Fatalf("contact %v %v", contact, err)
	}
}

func TestIncomingCallMediaDTMFAndRemoteHangup(t *testing.T) {
	e := setup(t, nil)
	type callResult struct {
		d   *diago.DialogClientSession
		med *diago.DialogMedia
		err error
	}
	answered := make(chan callResult, 1)
	go func() {
		d, med, err := e.box.Call(context.Background(), "0301234567", "Oma")
		answered <- callResult{d, med, err}
	}()

	call := e.nextIncoming(t)
	if call.Caller() != "0301234567" || call.CallerName() != "Oma" || call.Codec() != codec.G722 {
		t.Fatalf("caller=%q name=%q codec=%v", call.Caller(), call.CallerName(), call.Codec())
	}
	if err := call.Ringing(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sipMedia, err := call.Answer(ctx, call.Codec())
	if err != nil {
		t.Fatal(err)
	}
	var res callResult
	select {
	case res = <-answered:
	case <-time.After(timeout):
		t.Fatal("box call not answered")
	}
	if res.err != nil {
		t.Fatal(res.err)
	}
	answer := string(res.d.InviteResponse.Body())
	if !strings.Contains(answer, "RTP/AVP 9 101") || strings.Contains(answer, "PCMA") {
		t.Fatalf("answer must contain exactly G722 + telephone-event:\n%s", answer)
	}

	// Box → bridge.
	boxRTP := res.med.RTPSession()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			_ = boxRTP.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 9, SequenceNumber: uint16(i), Timestamp: uint32(i * 160), SSRC: 0xF1}, Payload: []byte("fritz")})
		}
	}()
	readRTP(t, sipMedia.ReadRTP, func(p *rtp.Packet) bool { return bytes.Equal(p.Payload, []byte("fritz")) })

	// Bridge → box, then DTMF in the same stream.
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			_ = sipMedia.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 9, SequenceNumber: uint16(100 + i), Timestamp: uint32(i * 160), SSRC: 0xB1}, Payload: []byte("phone")})
		}
	}()
	audio := readRTP(t, boxRTP.ReadRTP, func(p *rtp.Packet) bool { return bytes.Equal(p.Payload, []byte("phone")) })
	dtmfDone := make(chan error, 1)
	go func() { dtmfDone <- sipMedia.SendDTMF("5") }()
	// diago's RTPSession drops payload type switches within one SSRC, so
	// read below it (a real FRITZ!Box handles telephone-event in-stream).
	dtmf := readRTP(t, res.med.MediaSession().ReadRTP, func(p *rtp.Packet) bool { return p.PayloadType == 101 })
	if dtmf.Payload[0] != 5 || dtmf.SSRC != audio.SSRC {
		t.Fatalf("DTMF event %v ssrc %x, audio ssrc %x", dtmf.Payload, dtmf.SSRC, audio.SSRC)
	}
	if err := <-dtmfDone; err != nil {
		t.Fatalf("SendDTMF: %v", err)
	}

	// FRITZ!Box hangs up.
	if err := res.d.Hangup(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-call.Done():
	case <-time.After(timeout):
		t.Fatal("bridge did not see BYE")
	}
	if r := call.EndReason(); r != calls.SIPEndRemoteHangup {
		t.Fatalf("end reason %v", r)
	}
}

// The watch (websocket-pcma, v1.1) answers the same G.722-first INVITE with
// PCMA: the 200 OK must contain exactly PCMA and audio flows as PCMA.
func TestIncomingCallAnsweredWithPCMA(t *testing.T) {
	e := setup(t, nil)
	type callResult struct {
		d   *diago.DialogClientSession
		med *diago.DialogMedia
		err error
	}
	answered := make(chan callResult, 1)
	go func() {
		d, med, err := e.box.Call(context.Background(), "0301234567", "")
		answered <- callResult{d, med, err}
	}()
	call := e.nextIncoming(t)
	if call.Codec() != codec.G722 || !call.Offers(codec.PCMA) || !call.Offers(codec.PCMU) {
		t.Fatalf("codec %v, offers PCMA %v", call.Codec(), call.Offers(codec.PCMA))
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sipMedia, err := call.Answer(ctx, codec.PCMA)
	if err != nil {
		t.Fatal(err)
	}
	res := <-answered
	if res.err != nil {
		t.Fatal(res.err)
	}
	answer := string(res.d.InviteResponse.Body())
	if !strings.Contains(answer, "RTP/AVP 8 101") || strings.Contains(answer, "G722") {
		t.Fatalf("answer must contain exactly PCMA + telephone-event:\n%s", answer)
	}

	boxRTP := res.med.RTPSession()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			_ = boxRTP.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: uint16(i), Timestamp: uint32(i * 160), SSRC: 0xF2}, Payload: []byte("alaw-box")})
			_ = sipMedia.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: uint16(i), Timestamp: uint32(i * 160), SSRC: 0xB2}, Payload: []byte("alaw-watch")})
		}
	}()
	readRTP(t, sipMedia.ReadRTP, func(p *rtp.Packet) bool { return p.PayloadType == 8 && bytes.Equal(p.Payload, []byte("alaw-box")) })
	readRTP(t, boxRTP.ReadRTP, func(p *rtp.Packet) bool { return p.PayloadType == 8 && bytes.Equal(p.Payload, []byte("alaw-watch")) })
	_ = res.d.Hangup(ctx)
}

func TestAnswerWithCodecNotOffered(t *testing.T) {
	e := setup(t, []media.Codec{fakefritz.G722, media.CodecTelephoneEvent8000})
	go func() { _, _, _ = e.box.Call(context.Background(), "0301234567", "") }()
	call := e.nextIncoming(t)
	if call.Offers(codec.PCMA) {
		t.Fatal("G.722-only INVITE must not offer PCMA")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if _, err := call.Answer(ctx, codec.PCMA); err == nil {
		t.Fatal("answering with a codec that was not offered must fail")
	}
	_ = call.Reject(488, "Not Acceptable Here")
}

func TestIncomingCallCancelled(t *testing.T) {
	e := setup(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := e.box.Call(ctx, "123", "")
		done <- err
	}()
	call := e.nextIncoming(t)
	if err := call.Ringing(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // CANCEL needs a provisional response first
	cancel()
	select {
	case <-call.Done():
	case <-time.After(timeout):
		t.Fatal("bridge did not see CANCEL")
	}
	if r := call.EndReason(); r != calls.SIPEndCancelled {
		t.Fatalf("end reason %v", r)
	}
	<-done
}

func TestIncomingWithoutCommonCodecGets488(t *testing.T) {
	g729 := media.Codec{Name: "G729", PayloadType: 18, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1}
	e := setup(t, []media.Codec{g729})
	_, _, err := e.box.Call(context.Background(), "123", "")
	var resErr *sipgoResponseError
	if !asResponse(err, &resErr) || resErr.status != sip.StatusNotAcceptableHere {
		t.Fatalf("expected 488, got %v", err)
	}
	select {
	case <-e.incoming:
		t.Fatal("handler must not be called")
	default:
	}
}

func TestOutgoingBusy(t *testing.T) {
	e := setup(t, nil)
	e.box.SetOnInvite(func(d *diago.DialogServerSession) {
		_ = d.DialogServerSession.Respond(sip.StatusBusyHere, "Busy Here", nil)
	})
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, _, err := e.leg.Dial(ctx, "123", codec.G722, calls.DialEvents{})
	var de *calls.DialError
	if !errors.As(err, &de) || de.Status != 486 {
		t.Fatalf("expected DialError 486, got %v", err)
	}
}

func TestOutgoingCancelledByContext(t *testing.T) {
	e := setup(t, nil)
	e.box.SetOnInvite(func(d *diago.DialogServerSession) {
		_ = d.Ringing()
		<-d.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	_, _, err := e.leg.Dial(ctx, "123", codec.G722, calls.DialEvents{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

type sipgoResponseError struct{ status int }

// asResponse extracts the status of a final SIP error response (sipgo
// returns ErrDialogResponse by value or pointer depending on the path).
func asResponse(err error, out **sipgoResponseError) bool {
	var ptr *sipgo.ErrDialogResponse
	if errors.As(err, &ptr) && ptr.Res != nil {
		*out = &sipgoResponseError{status: ptr.Res.StatusCode}
		return true
	}
	var val sipgo.ErrDialogResponse
	if errors.As(err, &val) && val.Res != nil {
		*out = &sipgoResponseError{status: val.Res.StatusCode}
		return true
	}
	return false
}
