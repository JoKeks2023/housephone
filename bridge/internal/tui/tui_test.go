package tui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

var t0 = time.Date(2026, 9, 29, 18, 0, 0, 0, time.Local)

type fakeAPI struct {
	removed   []string
	renamed   string
	revoked   []string
	pairState admin.PairingState
}

func (f *fakeAPI) Status(context.Context) (admin.Status, error) {
	return admin.Status{BridgeName: "Zuhause", Version: "0.5.0", SIPRegistered: true, SIPUser: "620", Registrar: "192.168.0.1",
		PublicIP: "94.1.2.3", PublicIPSource: "fritzbox", MediaPort: 50000, APNsConfigured: true,
		DevicesTotal: 2, DevicesOnline: 1, StartedAt: t0.Add(-90 * time.Minute), Fingerprint: "AEbIfOzLcNeN", PublicURL: "wss://phone.x.de/v1/ws"}, nil
}
func (f *fakeAPI) Devices(context.Context) ([]admin.DeviceInfo, error) {
	return []admin.DeviceInfo{
		{ID: "i1", Name: "iPhone von Joris", Platform: "ios", Media: "webrtc", Push: "production", Online: true},
		{ID: "w1", Name: "Apple Watch", Platform: "watchos", Media: "websocket-pcma", PairedByName: "iPhone von Joris", LastSeen: t0},
	}, nil
}
func (f *fakeAPI) RenameDevice(_ context.Context, id, name string) (admin.DeviceInfo, error) {
	f.renamed = id + "=" + name
	return admin.DeviceInfo{ID: id, Name: name}, nil
}
func (f *fakeAPI) RemoveDevice(_ context.Context, id string, _ bool) (admin.RemoveResult, error) {
	f.removed = append(f.removed, id)
	return admin.RemoveResult{Removed: []admin.DeviceInfo{{ID: id}}}, nil
}
func (f *fakeAPI) CreatePairing(context.Context, string) (admin.PairingInfo, error) {
	return admin.PairingInfo{Code: "K7P2XH9QRMW4DZT8", Grouped: "K7P2-XH9Q-RMW4-DZT8", Link: "housephone://pair?v=2&code=K7P2XH9QRMW4DZT8", Fingerprint: "AEbIfOzLcNeN", ExpiresAt: t0.Add(10 * time.Minute)}, nil
}
func (f *fakeAPI) WaitPairing(context.Context, string) (admin.PairingState, error) {
	return f.pairState, nil
}
func (f *fakeAPI) RevokePairing(_ context.Context, code string) error {
	f.revoked = append(f.revoked, code)
	return nil
}
func (f *fakeAPI) Calls(context.Context) (admin.CallsView, error) {
	return admin.CallsView{Recent: []admin.CallInfo{{Direction: "incoming", Number: "…563", Codec: "PCMA", StartedAt: t0, ConnectedAt: t0, EndedAt: t0.Add(42 * time.Second), Reason: "remote_hangup"}}}, nil
}
func (f *fakeAPI) Stats(context.Context) (admin.Stats, error) {
	c := admin.Counters{Incoming: 3, Answered: 2, Missed: 1, Outgoing: 1, Codecs: map[string]int{"PCMA": 2}}
	return admin.Stats{Total: c, Today: c}, nil
}
func (f *fakeAPI) Logs(_ context.Context, after uint64) (admin.LogsView, error) {
	return admin.LogsView{Lines: []admin.LogLine{
		{Seq: 1, At: t0, Level: "INFO", Text: "registered at FRITZ!Box component=sip"},
		{Seq: 2, At: t0, Level: "WARN", Text: "push failed device=i1"},
	}, Next: 2}, nil
}
func (f *fakeAPI) SelfTest(context.Context) ([]admin.Check, error) {
	return admin.RunChecks(admin.CheckInput{RegistrarIP: "192.168.0.1", SIPUser: "620", PublicURL: "wss://phone.example.com/v1/ws", MediaPort: 50000}), nil
}
func (f *fakeAPI) Config(context.Context) (map[string]any, error) {
	return map[string]any{"sip": map[string]any{"password": "••••••", "username": "620"}}, nil
}

// drive runs a command chain synchronously (batches expanded, ticks dropped).
func drive(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 100; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		switch msg := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
			continue
		case tickMsg, nil:
			continue
		}
		var next tea.Model
		next, c = m.Update(msg)
		m = next.(Model)
		queue = append(queue, c)
	}
	return m
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, cmd := m.Update(msg)
		m = drive(t, next.(Model), cmd)
	}
	return m
}

func newTest(t *testing.T, api *fakeAPI) Model {
	m := New(api, func() time.Time { return t0 })
	m.width, m.height = 80, 40
	return drive(t, m, m.Init())
}

func TestOverview(t *testing.T) {
	m := newTest(t, &fakeAPI{})
	v := plain(m.View())
	for _, want := range []string{"1 Übersicht", "angemeldet als 620 an 192.168.0.1", "94.1.2.3 (fritzbox, Medien UDP 50000)", "2 gekoppelt, 1 online", "läuft seit 1h30m0s"} {
		if !strings.Contains(v, want) {
			t.Fatalf("overview misses %q:\n%s", want, v)
		}
	}
	m = press(t, m, "k")
	if v := plain(m.View()); !strings.Contains(v, "password=••••••") {
		t.Fatalf("config view:\n%s", v)
	}
}

func TestDevicesRenameAndRemove(t *testing.T) {
	api := &fakeAPI{}
	m := press(t, newTest(t, api), "2")
	v := plain(m.View())
	if !strings.Contains(v, "iPhone von Joris") || strings.Contains(v, "über iPhone von Joris") || !strings.Contains(v, "online") {
		t.Fatalf("devices:\n%s", v)
	}
	m = press(t, m, "down")
	if v := plain(m.View()); !strings.Contains(v, "über iPhone von Joris") {
		t.Fatalf("companion detail:\n%s", v)
	}
	m = press(t, m, "x")
	if v := plain(m.View()); !strings.Contains(v, "„Apple Watch“ entfernen?") {
		t.Fatalf("confirm:\n%s", v)
	}
	m = press(t, m, "n")
	if len(api.removed) != 0 {
		t.Fatal("removed without confirmation")
	}
	m = press(t, m, "x", "j")
	if len(api.removed) != 1 || api.removed[0] != "w1" || !strings.Contains(plain(m.View()), "sofort getrennt") {
		t.Fatalf("removed %v\n%s", api.removed, plain(m.View()))
	}
	m.input.SetValue("")
	m = press(t, m, "r")
	m.input.SetValue("Uhr")
	m = press(t, m, "enter")
	if api.renamed != "w1=Uhr" {
		t.Fatalf("renamed %q", api.renamed)
	}
}

func TestPairingShowsQRAndResult(t *testing.T) {
	api := &fakeAPI{pairState: admin.PairingState{Used: true, Device: &admin.DeviceInfo{ID: "i2", Name: "iPhone 17", Platform: "ios", KeyFingerprint: "D2A4F6"}}}
	m := press(t, newTest(t, api), "3", "n", "enter")
	v := plain(m.View())
	if !strings.Contains(v, "✓ Gekoppelt: iPhone 17") || !strings.Contains(v, "D2A4F6") {
		t.Fatalf("pairing result:\n%s", v)
	}
	// While waiting, the QR code and the grouped code are shown; Esc revokes.
	api.pairState = admin.PairingState{}
	m = newTest(t, api)
	m = press(t, m, "3", "n")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	p, _ := api.CreatePairing(context.Background(), "")
	next, _ = m.Update(pairingMsg(p))
	m = next.(Model)
	v = plain(m.View())
	if !strings.Contains(v, "K7P2-XH9Q-RMW4-DZT8") || !strings.Contains(v, "▀") || !strings.Contains(v, "Warte auf das Gerät") {
		t.Fatalf("waiting view:\n%s", v)
	}
	m = press(t, m, "esc")
	if len(api.revoked) != 1 {
		t.Fatalf("revoked %v", api.revoked)
	}
}

func TestCallsLogsAndSelfTest(t *testing.T) {
	m := press(t, newTest(t, &fakeAPI{}), "4")
	v := plain(m.View())
	if !strings.Contains(v, "eingehend 3 · angenommen 2 · verpasst 1") || !strings.Contains(v, "…563") || !strings.Contains(v, "42s") {
		t.Fatalf("calls:\n%s", v)
	}
	m = press(t, m, "5")
	if v := plain(m.View()); !strings.Contains(v, "registered at FRITZ!Box") {
		t.Fatalf("logs:\n%s", v)
	}
	m = press(t, m, "l", "l") // WARN+
	if v := plain(m.View()); strings.Contains(v, "registered at") || !strings.Contains(v, "push failed") {
		t.Fatalf("filtered logs:\n%s", v)
	}
	m = press(t, m, "6")
	v = plain(m.View())
	if !strings.Contains(v, "Es fehlt noch etwas.") || !strings.Contains(v, "Beispielwert") || !strings.Contains(v, "FRITZ!Box-Anmeldung: IP-Telefon") {
		t.Fatalf("selftest:\n%s", v)
	}
	m = press(t, m, "down", "down", "down", "down", "down")
	if v := plain(m.View()); !strings.Contains(v, "Öffentliche Adresse: Eigenen Hostnamen") {
		t.Fatalf("selftest hint:\n%s", v)
	}
}

func TestSnapshotWithoutTerminal(t *testing.T) {
	var b strings.Builder
	if err := Snapshot(&fakeAPI{}, &b); err != nil {
		t.Fatal(err)
	}
	v := plain(b.String())
	for _, want := range []string{"angemeldet als 620", "Selbsttest: fail", "[fail] Öffentliche Adresse", "→ Eigenen Hostnamen"} {
		if !strings.Contains(v, want) {
			t.Fatalf("snapshot misses %q:\n%s", want, v)
		}
	}
}

func TestFitsInto80x24(t *testing.T) {
	m := newTest(t, &fakeAPI{})
	m.width, m.height = 80, 24
	for _, key := range []string{"1", "2", "4", "5", "6", "?"} {
		m = press(t, m, key)
		v := plain(m.View())
		lines := strings.Split(v, "\n")
		if len(lines) > 24 {
			t.Fatalf("tab %s: %d lines\n%s", key, len(lines), v)
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > 80 {
				t.Fatalf("tab %s: line %d wide: %q", key, w, l)
			}
		}
		if key == "?" {
			m = press(t, m, "?")
		}
	}
}
