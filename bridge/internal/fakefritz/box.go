// Package fakefritz is a minimal FRITZ!Box stand-in for tests: a SIP
// registrar with digest authentication that can call the registered phone
// and answer calls from it.
package fakefritz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// G722 as diago parses it from SDP.
var G722 = media.Codec{Name: "G722", PayloadType: 9, SampleRate: 8000, SampleDur: 20 * time.Millisecond, NumChannels: 1}

// Box is the fake FRITZ!Box.
type Box struct {
	Host     string
	Port     int
	Username string
	Password string

	dg     *diago.Diago
	digest *diago.DigestAuthServer

	mu        sync.Mutex
	contact   *sip.Uri
	registers int
	// Registered is closed on the first successful REGISTER.
	Registered chan struct{}
	regOnce    sync.Once

	onInvite func(d *diago.DialogServerSession)
	// Invites receives every authenticated INVITE request.
	Invites chan *sip.Request
}

// FreeUDPPort returns a currently unused UDP port on 127.0.0.1.
func FreeUDPPort() (int, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port, nil
}

// Start runs the box on 127.0.0.1 until ctx ends.
func Start(ctx context.Context, username, password string, codecs []media.Codec, log *slog.Logger) (*Box, error) {
	port, err := FreeUDPPort()
	if err != nil {
		return nil, err
	}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("fritzbox"), sipgo.WithUserAgentHostname("127.0.0.1"))
	if err != nil {
		return nil, err
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, err
	}
	b := &Box{
		Host: "127.0.0.1", Port: port, Username: username, Password: password,
		digest:     diago.NewDigestServer(),
		Registered: make(chan struct{}),
		Invites:    make(chan *sip.Request, 10),
	}
	srv.OnRegister(b.handleRegister)
	if codecs == nil {
		codecs = []media.Codec{G722, media.CodecAudioAlaw, media.CodecAudioUlaw, media.CodecTelephoneEvent8000}
	}
	b.dg = diago.NewDiago(ua,
		diago.WithServer(srv),
		diago.WithLogger(log),
		diago.WithTransport(diago.Transport{Transport: "udp", BindHost: b.Host, BindPort: port}),
		diago.WithMediaConfig(diago.MediaConfig{Codecs: codecs}),
	)
	if err := b.dg.ServeBackground(ctx, b.handleInvite); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Box) auth() diago.DigestAuth {
	return diago.DigestAuth{Username: b.Username, Password: b.Password, Realm: "fritz.box"}
}

func (b *Box) handleRegister(req *sip.Request, tx sip.ServerTransaction) {
	b.mu.Lock()
	b.registers++
	b.mu.Unlock()
	res, err := b.digest.AuthorizeRequest(req, b.auth())
	if err == nil && res.StatusCode == sip.StatusOK {
		if c := req.Contact(); c != nil {
			addr := c.Address
			b.mu.Lock()
			b.contact = &addr
			b.mu.Unlock()
			res.AppendHeader(c.Clone())
		}
		expires := sip.ExpiresHeader(300)
		res.AppendHeader(&expires)
		b.regOnce.Do(func() { close(b.Registered) })
	}
	_ = tx.Respond(res)
}

func (b *Box) handleInvite(d *diago.DialogServerSession) {
	if err := b.digest.AuthorizeDialog(d, b.auth()); err != nil {
		return // 401 sent; the phone retries with credentials
	}
	b.Invites <- d.InviteRequest
	b.mu.Lock()
	handler := b.onInvite
	b.mu.Unlock()
	if handler != nil {
		handler(d)
		return
	}
	_ = d.DialogServerSession.Respond(sip.StatusServiceUnavailable, "No handler", nil)
}

// SetOnInvite sets the handler for INVITEs from the phone (the bridge
// dialing out). Authentication has already succeeded when it is called.
func (b *Box) SetOnInvite(f func(d *diago.DialogServerSession)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onInvite = f
}

// RegisterCount returns how many REGISTER requests arrived.
func (b *Box) RegisterCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.registers
}

// Contact returns the registered contact of the phone.
func (b *Box) Contact() (sip.Uri, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.contact == nil {
		return sip.Uri{}, errors.New("nothing registered")
	}
	return *b.contact, nil
}

// Call rings the registered phone. It returns once the call is answered
// (or fails); cancelling ctx before that sends CANCEL.
func (b *Box) Call(ctx context.Context, fromUser, displayName string) (*diago.DialogClientSession, *diago.DialogMedia, error) {
	contact, err := b.Contact()
	if err != nil {
		return nil, nil, err
	}
	d, err := b.dg.NewDialog(contact, diago.NewDialogOptions{})
	if err != nil {
		return nil, nil, err
	}
	d.InviteRequest.AppendHeader(&sip.FromHeader{
		DisplayName: displayName,
		Address:     sip.Uri{Scheme: "sip", User: fromUser, Host: b.Host, Port: b.Port},
		Params:      sip.NewParams(),
	})
	med, err := d.Invite(ctx, diago.InviteClientOptions{})
	if err != nil {
		_ = d.Close()
		return nil, nil, err
	}
	if err := d.Ack(ctx); err != nil {
		return nil, nil, err
	}
	return d, med, nil
}

// Addr returns "host:port".
func (b *Box) Addr() string { return net.JoinHostPort(b.Host, strconv.Itoa(b.Port)) }

func (b *Box) String() string { return fmt.Sprintf("fakefritz(%s)", b.Addr()) }
