package app_test

import (
	"context"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// ADR-0008 end to end: two profiles are two IP phones at the (fake)
// FRITZ!Box. A call to profile B's phone rings only B's device; B calls
// out over its own phone.

func withProfileB(t *testing.T) func(*config.Config) {
	port := freeUDP(t)
	return func(c *config.Config) {
		c.Profile.Name = "Profil A"
		c.Lines = []config.Line{{
			ID: "b", Name: "Profil B",
			SIP:     config.LineSIP{Username: "621", Password: "geheim-b", BindPort: port},
			Numbers: []string{"030 1234568"},
		}}
	}
}

// pairInto pairs a device into a profile the way the TUI does.
func (w *world) pairInto(t *testing.T, profileID string) *device {
	t.Helper()
	pc, err := w.bridge.Pairing.CreateFor("Test", profileID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	link, err := hp2.ParsePairingLink(app.PairingLink(w.url, w.lanURL, hp2.GroupCode(pc.Code), w.bridge.Key.Fingerprint(), "Zuhause"))
	if err != nil {
		t.Fatal(err)
	}
	pairBase, _ := hp2.HTTPBase(link.LAN)
	base, _ := hp2.HTTPBase(link.URL)
	key, err := hp2.NewSoftwareKey()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	res, err := hp2.Pair(ctx, http.DefaultClient, pairBase, key, link.Code, link.Fingerprint, "Test", protocol.PlatformIOS, "iPhone17,1")
	if err != nil {
		t.Fatalf("pairing: %v", err)
	}
	d := w.connect(t, &hp2.Client{BaseURL: base, DeviceID: res.DeviceID, BridgeID: res.BridgeID, Key: key, BridgePub: res.BridgePub})
	d.send(protocol.TypeHello, protocol.Hello{AppVersion: "e2e", Platform: protocol.PlatformIOS, PushToken: "a1b2c3d4" + hex.EncodeToString([]byte(profileID)), PushEnvironment: protocol.PushEnvironmentDevelopment})
	d.expect(protocol.TypeWelcome, &d.welcome)
	return d
}

func TestEndToEndProfilesRingSeparately(t *testing.T) {
	w := startWorld(t, withProfileB(t))
	a := w.pairInto(t, "")
	b := w.pairInto(t, "b")
	if a.welcome.Profile == nil || a.welcome.Profile.Name != "Profil A" || b.welcome.Profile == nil || b.welcome.Profile.ID != "b" || !b.welcome.SIPRegistered {
		t.Fatalf("welcome A %+v, B %+v", a.welcome.Profile, b.welcome.Profile)
	}

	// The FRITZ!Box rings profile B's IP phone.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := w.box.CallPhone(ctx, "621", "0301234567", "Anrufer")
		done <- err
	}()
	var incoming protocol.CallIncoming
	b.expect(protocol.TypeCallIncoming, &incoming)
	select {
	case push := <-w.pusher.pushes:
		if push.CallID != incoming.CallID {
			t.Fatalf("push %+v", push)
		}
	case <-time.After(wait):
		t.Fatal("no push for B")
	}
	select {
	case env := <-a.msgs:
		t.Fatalf("profile A got %s %s", env.Type, env.Payload)
	case push := <-w.pusher.pushes:
		t.Fatalf("second push %+v: A must not be woken", push)
	case <-time.After(300 * time.Millisecond):
	}
	// A cannot take B's call even with its ID.
	a.send(protocol.TypeCallAttach, protocol.CallAttach{CallID: incoming.CallID})
	var ended protocol.CallEnded
	a.expect(protocol.TypeCallEnded, &ended)
	if ended.Reason != protocol.EndReasonNotFound {
		t.Fatalf("A attached to B's call: %+v", ended)
	}
	cancel()
	<-done
	b.expect(protocol.TypeCallEnded, nil)
}

func TestEndToEndProfileCallsOutOverItsOwnPhone(t *testing.T) {
	w := startWorld(t, withProfileB(t))
	b := w.pairInto(t, "b")
	w.box.SetOnInvite(func(dlg *diago.DialogServerSession) {
		_ = dlg.Respond(486, "Busy Here", nil)
	})
	callID := uuid.NewString()
	b.send(protocol.TypeCallDial, protocol.CallDial{CallID: callID, Number: "0301234569"})
	var offer protocol.CallOffer
	b.expect(protocol.TypeCallOffer, &offer)
	b.answerOffer(callID, offer.SDP, codec.PCMA)
	select {
	case invite := <-w.box.Invites:
		if from := invite.From(); from == nil || from.Address.User != "621" {
			t.Fatalf("dialed from %v, want profile B's phone 621", invite.From())
		}
	case <-time.After(wait):
		t.Fatal("no INVITE")
	}
}

func TestEndToEndMoveDevice(t *testing.T) {
	w := startWorld(t, withProfileB(t))
	a := w.pairInto(t, "")
	client, err := admin.Dial(w.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := client.Devices(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices %+v %v", devices, err)
	}
	if _, err := client.MoveDevice(context.Background(), devices[0].ID, "b"); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-a.closed:
		if int(code) != protocol.CloseProfileChanged {
			t.Fatalf("close %d, want %d", code, protocol.CloseProfileChanged)
		}
	case <-time.After(wait):
		t.Fatal("moved device not reconnected")
	}
	again := w.connect(t, a.client)
	again.send(protocol.TypeHello, protocol.Hello{AppVersion: "e2e", Platform: protocol.PlatformIOS})
	again.expect(protocol.TypeWelcome, &again.welcome)
	if again.welcome.Profile == nil || again.welcome.Profile.ID != "b" {
		t.Fatalf("welcome after the move: %+v", again.welcome.Profile)
	}
	profiles, err := client.Profiles(context.Background())
	if err != nil || len(profiles) != 2 || !profiles[1].Registered || profiles[1].Devices != 1 {
		t.Fatalf("profiles %+v %v", profiles, err)
	}
}
