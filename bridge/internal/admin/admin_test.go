package admin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/calls"
	"github.com/JoKeks2023/housephone/bridge/internal/logsafe"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestLogRingKeepsLastLinesAndFormatsAttrs(t *testing.T) {
	ring := NewLogRing(3)
	log := slog.New(ring.Handler(slog.NewTextHandler(io.Discard, nil))).With("component", "sip").WithGroup("g")
	for i := range 5 {
		log.Info("line", "n", i, "text", "two words")
	}
	lines, next := ring.Since(0)
	if len(lines) != 3 || next != 5 || lines[0].Seq != 3 {
		t.Fatalf("lines %+v next %d", lines, next)
	}
	if lines[2].Text != `line component=sip g.n=4 g.text="two words"` || lines[2].Level != "INFO" {
		t.Fatalf("text %q", lines[2].Text)
	}
	if more, _ := ring.Since(5); len(more) != 0 {
		t.Fatalf("since 5: %+v", more)
	}
}

func TestRecorderCountsAndMasks(t *testing.T) {
	logsafe.SetShowNumbers(false)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.Local)
	r := NewRecorder(2, func() time.Time { return now })
	ev := func(kind calls.EventKind, id, dir string) calls.Event {
		return calls.Event{Kind: kind, At: now, CallID: id, Direction: dir, Number: "0301234563", Name: "Oma", Codec: "PCMA"}
	}
	// Answered incoming.
	r.Handle(ev(calls.EventStarted, "a", "incoming"))
	r.Handle(ev(calls.EventConnected, "a", "incoming"))
	r.Handle(ev(calls.EventConnected, "a", "incoming")) // re-attach: counted once
	r.Handle(ev(calls.EventEnded, "a", "incoming"))
	// Missed incoming.
	r.Handle(ev(calls.EventStarted, "b", "incoming"))
	r.Handle(ev(calls.EventEnded, "b", "incoming"))
	// Outgoing, still active.
	r.Handle(ev(calls.EventStarted, "c", "outgoing"))
	r.Handle(calls.Event{Kind: calls.EventPushFailed, At: now, DeviceID: "d", Error: "BadDeviceToken"})
	r.Handle(calls.Event{Kind: calls.EventMediaFailed, At: now, CallID: "c"})

	s := r.Stats()
	c := s.Total
	if c.Incoming != 2 || c.Answered != 1 || c.Missed != 1 || c.Outgoing != 1 || c.PushFailures != 1 || c.MediaFailures != 1 || c.Codecs["PCMA"] != 1 {
		t.Fatalf("total %+v", c)
	}
	if s.Today.Incoming != 2 || s.LastPush == nil || s.LastPush.OK {
		t.Fatalf("today %+v push %+v", s.Today, s.LastPush)
	}
	active, recent := r.Calls()
	if len(active) != 1 || len(recent) != 2 || recent[0].ID != "b" {
		t.Fatalf("active %+v recent %+v", active, recent)
	}
	for _, ci := range append(active, recent...) {
		if ci.Number != "…563" || ci.Name != "" {
			t.Fatalf("not masked: %+v", ci)
		}
	}
	if !recent[0].HasName {
		t.Fatal("HasName lost")
	}

	// The next day starts from zero; the total keeps counting.
	now = now.Add(24 * time.Hour)
	r.Handle(ev(calls.EventStarted, "d", "incoming"))
	s = r.Stats()
	if s.Today.Incoming != 1 || s.Total.Incoming != 3 {
		t.Fatalf("day roll: today %+v total %+v", s.Today, s.Total)
	}
}

func TestRunChecks(t *testing.T) {
	ok := CheckInput{
		Registrar: "192.168.0.1", RegistrarIP: "192.168.0.1", SIPRegistered: true, SIPUser: "620",
		FritzBoxConfigured: true, FritzBoxFeatures: []string{"a", "b"},
		APNsConfigured: true, APNsKeyLoaded: true, LastPush: &PushInfo{OK: true},
		PublicIP: "94.1.2.3", PublicIPSource: "fritzbox", PublicURL: "wss://phone.jokeks.de/v1/ws",
		MediaPort: 50000, DevicesTotal: 1, DevicesWithPush: 1,
	}
	checks := RunChecks(ok)
	for _, c := range checks {
		if c.Name == "Medienport" {
			if c.State != CheckWarn {
				t.Fatalf("media port %+v", c)
			}
			continue
		}
		if c.State != CheckOK {
			t.Fatalf("expected ok: %+v", c)
		}
	}
	if Worst(checks) != CheckWarn {
		t.Fatal("worst")
	}

	bad := ok
	bad.SIPRegistered = false
	bad.APNsConfigured = false
	bad.PublicIP = ""
	bad.PublicURL = "wss://phone.example.com/v1/ws"
	bad.DevicesWithPush = 0
	fails := map[string]bool{}
	for _, c := range RunChecks(bad) {
		if c.State == CheckFail {
			fails[c.Name] = true
			if c.Hint == "" {
				t.Fatalf("failing check without hint: %+v", c)
			}
		}
	}
	for _, name := range []string{"FRITZ!Box-Anmeldung", "Push (APNs)", "Öffentliche IP", "Öffentliche Adresse"} {
		if !fails[name] {
			t.Fatalf("%s should fail: %v", name, fails)
		}
	}
	bad.PublicURL = "ws://192.168.0.5:8080/v1/ws"
	for _, c := range RunChecks(bad) {
		if c.Name == "Öffentliche Adresse" && c.State != CheckWarn {
			t.Fatalf("ws:// %+v", c)
		}
	}
}

type fakeService struct {
	mu      sync.Mutex
	used    bool
	removed []string
}

func (f *fakeService) Status() Status { return Status{BridgeName: "Test", SIPRegistered: true} }
func (f *fakeService) Devices() ([]DeviceInfo, error) {
	return []DeviceInfo{{ID: "d1", Name: "iPhone"}}, nil
}
func (f *fakeService) RenameDevice(id, name string) (DeviceInfo, error) {
	if id != "d1" {
		return DeviceInfo{}, ErrNotFound
	}
	return DeviceInfo{ID: id, Name: name}, nil
}
func (f *fakeService) RemoveDevice(id string, keep bool) (RemoveResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != "d1" {
		return RemoveResult{}, ErrNotFound
	}
	f.removed = append(f.removed, id)
	return RemoveResult{Removed: []DeviceInfo{{ID: id}}}, nil
}
func (f *fakeService) CreatePairing(name string) (PairingInfo, error) {
	return PairingInfo{Code: "ABCD", Link: "housephone://pair?v=2"}, nil
}
func (f *fakeService) PairingState(code string) (PairingState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.used {
		return PairingState{Used: true, Device: &DeviceInfo{ID: "d2"}}, nil
	}
	return PairingState{}, nil
}
func (f *fakeService) RevokePairing(string) error { return nil }
func (f *fakeService) LanPairings() []LanPairingRequest {
	return []LanPairingRequest{{ID: "r1", DeviceName: "iPhone", SAS: "123456"}}
}
func (f *fakeService) ApproveLanPairing(id string) (DeviceInfo, error) {
	if id != "r1" {
		return DeviceInfo{}, ErrNotFound
	}
	return DeviceInfo{ID: "d3", Name: "iPhone"}, nil
}
func (f *fakeService) DenyLanPairing(id string) error {
	if id != "r1" {
		return ErrNotFound
	}
	return nil
}
func (f *fakeService) Calls() CallsView                 { return CallsView{} }
func (f *fakeService) Stats() Stats                     { return Stats{} }
func (f *fakeService) Logs(after uint64) LogsView       { return LogsView{Next: after} }
func (f *fakeService) SelfTest(context.Context) []Check { return []Check{{Name: "x", State: CheckOK}} }
func (f *fakeService) Config() map[string]any {
	return map[string]any{"sip": map[string]any{"password": "••••••"}}
}

func shortDir(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "hpadm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestServerOverSocket(t *testing.T) {
	dir := shortDir(t)
	// A stale socket file from a crash is replaced.
	if err := os.WriteFile(filepath.Join(dir, SocketName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	svc := &fakeService{}
	srv, err := Listen(dir, svc, quiet())
	if err != nil {
		t.Fatal(err)
	}
	srv.PollInterval = 10 * time.Millisecond
	go srv.Serve()
	t.Cleanup(func() { srv.Close(context.Background()) })

	info, err := os.Stat(srv.Path())
	if err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket %v %v", info.Mode(), err)
	}
	if _, err := Listen(dir, svc, quiet()); err == nil || !strings.Contains(err.Error(), "schon benutzt") {
		t.Fatalf("second listen: %v", err)
	}

	c, err := Dial(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if st, err := c.Status(ctx); err != nil || st.BridgeName != "Test" {
		t.Fatalf("status %+v %v", st, err)
	}
	if d, err := c.RenameDevice(ctx, "d1", "Neu"); err != nil || d.Name != "Neu" {
		t.Fatalf("rename %+v %v", d, err)
	}
	if _, err := c.RenameDevice(ctx, "nope", "x"); err != ErrNotFound {
		t.Fatalf("rename unknown: %v", err)
	}
	if _, err := c.RemoveDevice(ctx, "d1", false); err != nil || len(svc.removed) != 1 {
		t.Fatalf("remove %v %v", svc.removed, err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		svc.mu.Lock()
		svc.used = true
		svc.mu.Unlock()
	}()
	if st, err := c.WaitPairing(ctx, "ABCD"); err != nil || !st.Used || st.Device.ID != "d2" {
		t.Fatalf("wait %+v %v", st, err)
	}
	if l, err := c.LanPairings(ctx); err != nil || len(l) != 1 || l[0].SAS != "123456" {
		t.Fatalf("lan pairings %+v %v", l, err)
	}
	if d, err := c.ApproveLanPairing(ctx, "r1"); err != nil || d.ID != "d3" {
		t.Fatalf("approve %+v %v", d, err)
	}
	if _, err := c.ApproveLanPairing(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("approve unknown: %v", err)
	}
	if err := c.DenyLanPairing(ctx, "r1"); err != nil {
		t.Fatalf("deny %v", err)
	}
	if cfg, err := c.Config(ctx); err != nil || cfg["sip"].(map[string]any)["password"] != "••••••" {
		t.Fatalf("config %+v %v", cfg, err)
	}

	srv.Close(context.Background())
	if _, err := os.Stat(srv.Path()); !os.IsNotExist(err) {
		t.Fatalf("socket not removed: %v", err)
	}
	if _, err := Dial(dir); err != ErrNotRunning {
		t.Fatalf("dial after close: %v", err)
	}
}

func TestListenRejectsTooLongPath(t *testing.T) {
	dir := filepath.Join(shortDir(t), strings.Repeat("x", 100))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Skip(err)
	}
	if _, err := Listen(dir, &fakeService{}, quiet()); err == nil || !strings.Contains(err.Error(), "länger als") {
		t.Fatalf("long path: %v", err)
	}
}
