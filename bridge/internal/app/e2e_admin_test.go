package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/emiago/diago"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/codec"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// shortDataDir keeps the admin socket path under the 104-byte limit.
func shortDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hpa")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func dialAdmin(t *testing.T, dataDir string) *admin.Client {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		c, err := admin.Dial(dataDir)
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("admin socket: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Removing a device through the admin socket cuts its connection off at
// once (4003), without waiting for the periodic check.
func TestEndToEndAdminRemovesDeviceImmediately(t *testing.T) {
	w := startWorld(t, func(c *config.Config) { c.Bridge.DataDir = shortDataDir(t) })
	d := w.pairAndConnect(t)
	client := dialAdmin(t, w.dataDir)
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()

	info, err := os.Stat(filepath.Join(w.dataDir, admin.SocketName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket perm %v %v", info.Mode().Perm(), err)
	}
	devs, err := client.Devices(ctx)
	if err != nil || len(devs) != 1 || !devs[0].Online {
		t.Fatalf("devices %+v %v", devs, err)
	}
	st, err := client.Status(ctx)
	if err != nil || !st.SIPRegistered || st.DevicesOnline != 1 || st.Fingerprint == "" {
		t.Fatalf("status %+v %v", st, err)
	}

	start := time.Now()
	res, err := client.RemoveDevice(ctx, d.client.DeviceID, false)
	if err != nil || len(res.Removed) != 1 {
		t.Fatalf("remove %+v %v", res, err)
	}
	select {
	case status := <-d.closed:
		if status != websocket.StatusCode(protocol.CloseRevoked) {
			t.Fatalf("closed with %v, want 4003", status)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatalf("closed after %v, want immediately", time.Since(start))
		}
	case <-time.After(wait):
		t.Fatal("connection not closed")
	}
	if _, err := client.RemoveDevice(ctx, d.client.DeviceID, false); err != admin.ErrNotFound {
		t.Fatalf("second remove: %v", err)
	}
	checks, err := client.SelfTest(ctx)
	if err != nil || len(checks) == 0 || checks[0].State != admin.CheckOK {
		t.Fatalf("selftest %+v %v", checks, err)
	}
}

// The admin API reports calls and statistics.
func TestEndToEndAdminCallStats(t *testing.T) {
	w := startWorld(t, func(c *config.Config) { c.Bridge.DataDir = shortDataDir(t) })
	d := w.pairAndConnect(t)
	client := dialAdmin(t, w.dataDir)

	answered := make(chan *diago.DialogClientSession, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dlg, _, err := w.box.Call(ctx, "0301234567", "Oma")
		if err != nil {
			t.Error(err)
		}
		answered <- dlg
	}()
	var incoming protocol.CallIncoming
	d.expect(protocol.TypeCallIncoming, &incoming)
	d.send(protocol.TypeCallAttach, protocol.CallAttach{CallID: incoming.CallID})
	d.expect(protocol.TypeCallIncoming, nil)
	var offer protocol.CallOffer
	d.expect(protocol.TypeCallOffer, &offer)
	d.answerOffer(incoming.CallID, offer.SDP, codec.G722)
	d.send(protocol.TypeCallAccept, protocol.CallAccept{CallID: incoming.CallID})
	dlg := <-answered
	d.expect(protocol.TypeCallState, nil)

	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	s, err := client.Stats(ctx)
	if err != nil || s.Total.Incoming != 1 || s.Total.Answered != 1 || s.Total.Codecs["G722"] != 1 {
		t.Fatalf("stats %+v %v", s, err)
	}
	calls, err := client.Calls(ctx)
	if err != nil || len(calls.Active) != 1 || calls.Active[0].ConnectedAt.IsZero() {
		t.Fatalf("calls %+v %v", calls, err)
	}
	if n := calls.Active[0].Number; n != "…567" {
		t.Fatalf("number not masked: %q", n)
	}
	if calls.Active[0].Name != "" || !calls.Active[0].HasName {
		t.Fatalf("name leaked or missing: %+v", calls.Active[0])
	}

	if err := dlg.Hangup(ctx); err != nil {
		t.Fatal(err)
	}
	d.expect(protocol.TypeCallEnded, nil)
	deadline := time.Now().Add(wait)
	for {
		calls, err = client.Calls(ctx)
		if err == nil && len(calls.Recent) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("recent %+v %v", calls, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Recent[0].Reason != protocol.EndReasonRemoteHangup || len(calls.Active) != 0 {
		t.Fatalf("recent %+v", calls)
	}
}
