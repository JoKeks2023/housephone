// Package tui is the bridge's terminal admin interface. It runs as its own
// process (housephone-bridge tui, usually via docker compose exec) and talks
// to the running bridge over the admin socket.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mdp/qrterminal/v3"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

// API is the part of admin.Client the TUI uses (fakes in tests).
type API interface {
	Status(ctx context.Context) (admin.Status, error)
	Devices(ctx context.Context) ([]admin.DeviceInfo, error)
	RenameDevice(ctx context.Context, id, name string) (admin.DeviceInfo, error)
	RemoveDevice(ctx context.Context, id string, keepCompanions bool) (admin.RemoveResult, error)
	MoveDevice(ctx context.Context, id, profile string) (admin.MoveResult, error)
	CreatePairing(ctx context.Context, name, profile string) (admin.PairingInfo, error)
	WaitPairing(ctx context.Context, code string) (admin.PairingState, error)
	RevokePairing(ctx context.Context, code string) error
	LanPairings(ctx context.Context) ([]admin.LanPairingRequest, error)
	ApproveLanPairing(ctx context.Context, id, profile string) (admin.DeviceInfo, error)
	DenyLanPairing(ctx context.Context, id string) error
	Calls(ctx context.Context) (admin.CallsView, error)
	Stats(ctx context.Context) (admin.Stats, error)
	Logs(ctx context.Context, after uint64) (admin.LogsView, error)
	SelfTest(ctx context.Context) ([]admin.Check, error)
	Config(ctx context.Context) (map[string]any, error)
}

// Run starts the TUI in the terminal.
func Run(api API) error {
	_, err := tea.NewProgram(New(api, time.Now), tea.WithAltScreen()).Run()
	return err
}

type tab int

const (
	tabOverview tab = iota
	tabDevices
	tabPairing
	tabCalls
	tabLogs
	tabSelfTest
)

var tabNames = []string{"Übersicht", "Geräte", "Kopplung", "Anrufe", "Logs", "Selbsttest"}

type inputMode int

const (
	inputNone inputMode = iota
	inputRename
	inputSearch
	inputPairName
)

// Model is the TUI state.
type Model struct {
	api API
	now func() time.Time

	tab           tab
	width, height int
	help          bool
	showConfig    bool
	err           string
	flash         string

	status  admin.Status
	devices []admin.DeviceInfo
	cursor  int
	confirm string // device ID awaiting removal confirmation
	calls   admin.CallsView
	stats   admin.Stats
	logs    []admin.LogLine
	logNext uint64
	level   int // 0 all, 1 info+, 2 warn+, 3 error
	search  string
	checks  []admin.Check
	check   int // selected check; its hint is shown below the list
	config  map[string]any

	pairing     *admin.PairingInfo
	pairingQR   string
	pairingDone *admin.PairingState

	// lan are pairing requests from the home network (ADR-0007);
	// lanConfirm is the one whose SAS the admin is asked to confirm.
	lan        []admin.LanPairingRequest
	lanCursor  int
	lanConfirm string

	input     textinput.Model
	inputMode inputMode

	// picker asks which profile a device belongs to (ADR-0008); nil
	// unless the household has several profiles and a choice is pending.
	picker *profilePicker
}

// pickPurpose is what a profile choice is for.
type pickPurpose int

const (
	pickPair    pickPurpose = iota // new pairing code
	pickApprove                    // LAN pairing request
	pickMove                       // move a device
)

type profilePicker struct {
	purpose pickPurpose
	// target is the LAN request or device ID; name the new code's name.
	target, name string
	cursor       int
}

// New creates the model.
func New(api API, now func() time.Time) Model {
	in := textinput.New()
	in.CharLimit = 64
	return Model{api: api, now: now, input: in, width: 80, height: 24}
}

// Messages.
type (
	tickMsg    struct{}
	statusMsg  admin.Status
	devicesMsg []admin.DeviceInfo
	callsMsg   struct {
		calls admin.CallsView
		stats admin.Stats
	}
	logsMsg    admin.LogsView
	checksMsg  []admin.Check
	configMsg  map[string]any
	pairingMsg admin.PairingInfo
	pairedMsg  admin.PairingState
	lanMsg     []admin.LanPairingRequest
	flashMsg   string
	errMsg     error
)

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func call[T any](f func(ctx context.Context) (T, error), wrap func(T) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		v, err := f(ctx)
		if err != nil {
			return errMsg(err)
		}
		return wrap(v)
	}
}

// refresh loads the status and the data of the current tab.
func (m Model) refresh() tea.Cmd {
	cmds := []tea.Cmd{
		call(m.api.Status, func(s admin.Status) tea.Msg { return statusMsg(s) }),
		// Requests are announced on every tab, so they are loaded always.
		call(m.api.LanPairings, func(l []admin.LanPairingRequest) tea.Msg { return lanMsg(l) }),
	}
	switch m.tab {
	case tabOverview:
		if m.showConfig {
			cmds = append(cmds, call(m.api.Config, func(c map[string]any) tea.Msg { return configMsg(c) }))
		}
	case tabDevices:
		cmds = append(cmds, call(m.api.Devices, func(d []admin.DeviceInfo) tea.Msg { return devicesMsg(d) }))
	case tabCalls:
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, err := m.api.Calls(ctx)
			if err != nil {
				return errMsg(err)
			}
			s, err := m.api.Stats(ctx)
			if err != nil {
				return errMsg(err)
			}
			return callsMsg{calls: c, stats: s}
		})
	case tabLogs:
		after := m.logNext
		cmds = append(cmds, call(func(ctx context.Context) (admin.LogsView, error) { return m.api.Logs(ctx, after) }, func(v admin.LogsView) tea.Msg { return logsMsg(v) }))
	}
	return tea.Batch(cmds...)
}

func (m Model) runSelfTest() tea.Cmd {
	return call(m.api.SelfTest, func(c []admin.Check) tea.Msg { return checksMsg(c) })
}

func (m Model) waitPairing(code string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		st, err := m.api.WaitPairing(ctx, code)
		if err != nil {
			return errMsg(err)
		}
		return pairedMsg(st)
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())
	case statusMsg:
		m.status, m.err = admin.Status(msg), ""
	case devicesMsg:
		m.devices = msg
		if m.cursor >= len(m.devices) {
			m.cursor = max(0, len(m.devices)-1)
		}
	case callsMsg:
		m.calls, m.stats = msg.calls, msg.stats
	case logsMsg:
		m.logs = append(m.logs, msg.Lines...)
		if len(m.logs) > 1000 {
			m.logs = m.logs[len(m.logs)-1000:]
		}
		if msg.Next > m.logNext {
			m.logNext = msg.Next
		}
	case checksMsg:
		first := m.checks == nil
		m.checks = msg
		if first || m.check >= len(msg) {
			m.check = 0
			for i, c := range msg {
				if c.State != admin.CheckOK {
					m.check = i
					break
				}
			}
		}
	case configMsg:
		m.config = msg
	case pairingMsg:
		p := admin.PairingInfo(msg)
		m.pairing, m.pairingDone, m.pairingQR = &p, nil, renderQR(p.Link)
		return m, m.waitPairing(p.Code)
	case lanMsg:
		m.lan = msg
		if m.lanCursor >= len(m.lan) {
			m.lanCursor = max(0, len(m.lan)-1)
		}
		if m.lanConfirm != "" && !m.hasLan(m.lanConfirm) {
			m.lanConfirm = ""
			m.flash = "Die Anfrage ist abgelaufen."
		}
	case pairedMsg:
		st := admin.PairingState(msg)
		if m.pairing == nil {
			return m, nil
		}
		if !st.Used && !st.Expired {
			return m, m.waitPairing(m.pairing.Code)
		}
		m.pairingDone = &st
	case flashMsg:
		m.flash = string(msg)
		return m, m.refresh()
	case errMsg:
		m.err = msg.Error()
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.inputMode != inputNone {
		return m.inputKey(k)
	}
	if m.picker != nil {
		return m.pickerKey(k)
	}
	if m.lanConfirm != "" {
		id := m.lanConfirm
		m.lanConfirm = ""
		if k.String() == "j" || k.String() == "y" {
			if m.multiProfile() {
				m.picker = &profilePicker{purpose: pickApprove, target: id}
				return m, nil
			}
			return m, m.approveLan(id, "")
		}
		m.flash = "Nicht freigegeben – die Anfrage wartet weiter (d lehnt sie ab)."
		return m, nil
	}
	if m.confirm != "" {
		id := m.confirm
		m.confirm = ""
		if k.String() == "j" || k.String() == "y" {
			return m, call(func(ctx context.Context) (admin.RemoveResult, error) { return m.api.RemoveDevice(ctx, id, false) },
				func(r admin.RemoveResult) tea.Msg {
					return flashMsg(fmt.Sprintf("%d Gerät(e) entfernt und sofort getrennt.", len(r.Removed)))
				})
		}
		m.flash = "Entfernen abgebrochen."
		return m, nil
	}
	switch s := k.String(); s {
	case "q", "ctrl+c":
		if m.pairing != nil && m.pairingDone == nil {
			code := m.pairing.Code
			return m, tea.Sequence(func() tea.Msg { _ = m.api.RevokePairing(context.Background(), code); return nil }, tea.Quit)
		}
		return m, tea.Quit
	case "?":
		m.help = !m.help
	case "1", "2", "3", "4", "5", "6":
		m.tab = tab(s[0] - '1')
		m.flash = ""
		cmds := []tea.Cmd{m.refresh()}
		if m.tab == tabSelfTest && m.checks == nil {
			cmds = append(cmds, m.runSelfTest())
		}
		return m, tea.Batch(cmds...)
	case "tab", "right":
		m.tab = (m.tab + 1) % tab(len(tabNames))
		return m, m.refresh()
	case "shift+tab", "left":
		m.tab = (m.tab + tab(len(tabNames)) - 1) % tab(len(tabNames))
		return m, m.refresh()
	}
	switch m.tab {
	case tabOverview:
		if k.String() == "k" {
			m.showConfig = !m.showConfig
			return m, m.refresh()
		}
	case tabDevices:
		switch k.String() {
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(len(m.devices)-1, m.cursor+1)
		case "r":
			if len(m.devices) > 0 {
				m.inputMode = inputRename
				m.input.SetValue(m.devices[m.cursor].Name)
				m.input.Focus()
			}
		case "x", "delete":
			if len(m.devices) > 0 {
				m.confirm = m.devices[m.cursor].ID
			}
		case "p":
			if len(m.devices) == 0 || !m.multiProfile() {
				break
			}
			d := m.devices[m.cursor]
			if d.PairedBy != "" {
				m.flash = "Eine Uhr gehört immer zum Profil ihres iPhones – verschiebe das iPhone."
				break
			}
			m.picker = &profilePicker{purpose: pickMove, target: d.ID, cursor: m.profileIndex(d.Profile)}
		}
	case tabPairing:
		switch k.String() {
		case "up", "k":
			m.lanCursor = max(0, m.lanCursor-1)
		case "down", "j":
			m.lanCursor = min(len(m.lan)-1, m.lanCursor+1)
		case "a":
			if len(m.lan) > 0 {
				m.lanConfirm = m.lan[m.lanCursor].ID
			}
		case "d":
			if len(m.lan) > 0 {
				id := m.lan[m.lanCursor].ID
				return m, call(func(ctx context.Context) (struct{}, error) { return struct{}{}, m.api.DenyLanPairing(ctx, id) },
					func(struct{}) tea.Msg { return flashMsg("Anfrage abgelehnt.") })
			}
		case "n", "enter":
			m.inputMode = inputPairName
			m.input.SetValue("")
			m.input.Placeholder = "z. B. iPhone von Joris (optional)"
			m.input.Focus()
		case "esc":
			if m.pairing != nil && m.pairingDone == nil {
				code := m.pairing.Code
				m.pairing, m.pairingQR = nil, ""
				m.flash = "Code widerrufen."
				return m, func() tea.Msg { _ = m.api.RevokePairing(context.Background(), code); return nil }
			}
		}
	case tabLogs:
		switch k.String() {
		case "l":
			m.level = (m.level + 1) % 4
		case "/":
			m.inputMode = inputSearch
			m.input.SetValue(m.search)
			m.input.Focus()
		case "esc":
			m.search = ""
		}
	case tabSelfTest:
		switch k.String() {
		case "r":
			m.checks = nil
			return m, m.runSelfTest()
		case "up", "k":
			m.check = max(0, m.check-1)
		case "down", "j":
			m.check = min(len(m.checks)-1, m.check+1)
		}
	}
	return m, nil
}

func (m Model) inputKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.inputMode = inputNone
		m.input.Blur()
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		mode := m.inputMode
		m.inputMode = inputNone
		m.input.Blur()
		switch mode {
		case inputRename:
			if value == "" || len(m.devices) == 0 {
				return m, nil
			}
			id := m.devices[m.cursor].ID
			return m, call(func(ctx context.Context) (admin.DeviceInfo, error) { return m.api.RenameDevice(ctx, id, value) },
				func(d admin.DeviceInfo) tea.Msg { return flashMsg("Umbenannt in „" + d.Name + "“.") })
		case inputSearch:
			m.search = value
		case inputPairName:
			if m.multiProfile() {
				m.picker = &profilePicker{purpose: pickPair, name: value}
				return m, nil
			}
			return m, m.createPairing(value, "")
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// multiProfile reports whether the household has several profiles; only
// then is there something to choose.
func (m Model) multiProfile() bool { return len(m.status.Profiles) > 1 }

func (m Model) profileIndex(id string) int {
	for i, p := range m.status.Profiles {
		if p.ID == id {
			return i
		}
	}
	return 0
}

func (m Model) profileName(id string) string {
	for _, p := range m.status.Profiles {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

func (m Model) createPairing(name, profile string) tea.Cmd {
	return call(func(ctx context.Context) (admin.PairingInfo, error) { return m.api.CreatePairing(ctx, name, profile) },
		func(p admin.PairingInfo) tea.Msg { return pairingMsg(p) })
}

func (m Model) approveLan(id, profile string) tea.Cmd {
	return call(func(ctx context.Context) (admin.DeviceInfo, error) { return m.api.ApproveLanPairing(ctx, id, profile) },
		func(d admin.DeviceInfo) tea.Msg {
			return flashMsg("Gekoppelt: „" + d.Name + "“ · Profil " + d.ProfileName + ".")
		})
}

// pickerKey handles the profile choice.
func (m Model) pickerKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := *m.picker
	switch k.String() {
	case "up", "k":
		p.cursor = max(0, p.cursor-1)
	case "down", "j":
		p.cursor = min(len(m.status.Profiles)-1, p.cursor+1)
	case "esc":
		m.picker = nil
		m.flash = map[pickPurpose]string{
			pickPair:    "Kein Code erzeugt.",
			pickApprove: "Nicht freigegeben – die Anfrage wartet weiter (d lehnt sie ab).",
			pickMove:    "Nicht verschoben.",
		}[p.purpose]
		return m, nil
	case "enter":
		m.picker = nil
		if p.cursor >= len(m.status.Profiles) {
			return m, nil
		}
		profile := m.status.Profiles[p.cursor].ID
		switch p.purpose {
		case pickPair:
			return m, m.createPairing(p.name, profile)
		case pickApprove:
			return m, m.approveLan(p.target, profile)
		case pickMove:
			return m, call(func(ctx context.Context) (admin.MoveResult, error) { return m.api.MoveDevice(ctx, p.target, profile) },
				func(r admin.MoveResult) tea.Msg {
					return flashMsg(fmt.Sprintf("%d Gerät(e) nach „%s“ verschoben und neu verbunden.", len(r.Moved), m.profileName(profile)))
				})
		}
		return m, nil
	}
	m.picker = &p
	return m, nil
}

// viewPicker renders the profile choice.
func (m Model) viewPicker() string {
	p := m.picker
	question := map[pickPurpose]string{
		pickPair:    "Für welches Profil ist das neue Gerät?",
		pickApprove: "Zu welchem Profil gehört das Gerät?",
		pickMove:    "In welches Profil verschieben? (Uhren des Geräts wandern mit)",
	}[p.purpose]
	var b strings.Builder
	b.WriteString("\n" + boldS.Render(question) + "\n")
	for i, pr := range m.status.Profiles {
		marker := "  "
		if i == p.cursor {
			marker = selS.Render("› ")
		}
		fmt.Fprintf(&b, "%s%s %s\n", marker, pr.Name, mutedS.Render(strings.Join(pr.Numbers, ", ")))
	}
	b.WriteString(mutedS.Render("  ↑↓ auswählen · Enter bestätigen · Esc abbrechen") + "\n")
	return b.String()
}

func renderQR(link string) string {
	var b strings.Builder
	qrterminal.GenerateWithConfig(link, qrterminal.Config{
		Level: qrterminal.L, Writer: &b, HalfBlocks: true,
		BlackChar: qrterminal.BLACK_BLACK, WhiteBlackChar: qrterminal.WHITE_BLACK,
		WhiteChar: qrterminal.WHITE_WHITE, BlackWhiteChar: qrterminal.BLACK_WHITE,
		QuietZone: 1,
	})
	return strings.TrimRight(b.String(), "\n")
}

// Styles (design language: teal accent, muted neutrals).
var (
	accent = lipgloss.Color("#14B8A6")
	muted  = lipgloss.Color("#8B949E")
	okC    = lipgloss.Color("#3FB950")
	warnC  = lipgloss.Color("#D29922")
	failC  = lipgloss.Color("#F85149")
	titleS = lipgloss.NewStyle().Bold(true).Foreground(accent)
	mutedS = lipgloss.NewStyle().Foreground(muted)
	tabOn  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0D1117")).Background(accent).Padding(0, 1)
	tabOff = lipgloss.NewStyle().Foreground(muted).Padding(0, 1)
	selS   = lipgloss.NewStyle().Foreground(accent).Bold(true)
	okS    = lipgloss.NewStyle().Foreground(okC)
	warnS  = lipgloss.NewStyle().Foreground(warnC)
	failS  = lipgloss.NewStyle().Foreground(failC)
	boldS  = lipgloss.NewStyle().Bold(true)
)

func dot(state admin.CheckState) string {
	switch state {
	case admin.CheckOK:
		return okS.Render("●")
	case admin.CheckWarn:
		return warnS.Render("●")
	}
	return failS.Render("●")
}

func yes(ok bool, yesText, noText string) string {
	if ok {
		return okS.Render("● ") + yesText
	}
	return failS.Render("● ") + noText
}

// View renders the current tab.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleS.Render("Housephone Bridge") + mutedS.Render(" · "+m.status.BridgeName) + "\n")
	for i, name := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if tab(i) == m.tab {
			b.WriteString(tabOn.Render(label))
		} else {
			b.WriteString(tabOff.Render(label))
		}
	}
	b.WriteString("\n\n")
	if len(m.lan) > 0 && m.tab != tabPairing {
		b.WriteString(warnS.Render(fmt.Sprintf("  %d Kopplungsanfrage(n) aus dem Heimnetz – Taste 3", len(m.lan))) + "\n\n")
	}
	if m.help {
		b.WriteString(helpText)
	} else {
		switch m.tab {
		case tabOverview:
			b.WriteString(m.viewOverview())
		case tabDevices:
			b.WriteString(m.viewDevices())
		case tabPairing:
			b.WriteString(m.viewPairing())
		case tabCalls:
			b.WriteString(m.viewCalls())
		case tabLogs:
			b.WriteString(m.viewLogs())
		case tabSelfTest:
			b.WriteString(m.viewSelfTest())
		}
	}
	if m.picker != nil {
		b.WriteString(m.viewPicker())
	}
	if m.inputMode != inputNone {
		prompt := map[inputMode]string{inputRename: "Neuer Name: ", inputSearch: "Suche: ", inputPairName: "Gerätename: "}[m.inputMode]
		b.WriteString("\n" + prompt + m.input.View() + mutedS.Render("  (Enter bestätigt, Esc bricht ab)"))
	}
	b.WriteString("\n")
	if m.err != "" {
		b.WriteString(failS.Render("Fehler: "+m.err) + "\n")
	} else if m.flash != "" {
		b.WriteString(okS.Render(m.flash) + "\n")
	}
	b.WriteString(mutedS.Render("1–6 Ansicht · ? Hilfe · q Beenden"))
	return b.String()
}

const helpText = `Tasten
  1–6 / Tab     Ansicht wechseln          q    Beenden
  Übersicht     k  Konfiguration ein/aus
  Geräte        ↑↓ auswählen · r umbenennen · x entfernen (sofort getrennt)
                p  in ein anderes Profil verschieben (bei mehreren Profilen)
  Kopplung      n  neuer Code mit QR · Esc Code widerrufen
                a  Anfrage aus dem Heimnetz freigeben · d ablehnen
  Logs          l  Level (alle/info/warn/error) · /  suchen · Esc Suche löschen
  Selbsttest    ↑↓ Hinweis zur Prüfung · r  erneut prüfen

Die TUI spricht nur über /data/admin.sock mit der Bridge – ohne Netzwerk.
`

func (m Model) viewOverview() string {
	s := m.status
	var b strings.Builder
	up := "–"
	if !s.StartedAt.IsZero() {
		up = m.now().Sub(s.StartedAt).Round(time.Second).String()
	}
	if len(s.Profiles) > 1 {
		for i, p := range s.Profiles {
			label := "             "
			if i == 0 {
				label = "  Profile    "
			}
			numbers := "keine eigene Nummer"
			if len(p.Numbers) > 0 {
				numbers = strings.Join(p.Numbers, ", ")
			}
			fmt.Fprintf(&b, "%s%s %s\n", label, yes(p.Registered, boldS.Render(p.Name)+" · "+p.SIPUser, boldS.Render(p.Name)+" · "+p.SIPUser+" nicht angemeldet"),
				mutedS.Render(fmt.Sprintf("· %s · %d Gerät(e), %d online", numbers, p.Devices, p.DevicesOnline)))
		}
		fmt.Fprintf(&b, "  FRITZ!Box    %s\n", s.Registrar)
	} else {
		fmt.Fprintf(&b, "  FRITZ!Box    %s\n", yes(s.SIPRegistered, "angemeldet als "+s.SIPUser+" an "+s.Registrar, "nicht angemeldet an "+s.Registrar))
	}
	ip := s.PublicIP
	if ip == "" {
		ip = "unbekannt"
	}
	fmt.Fprintf(&b, "  Öffentl. IP  %s %s\n", ip, mutedS.Render("("+orDash(s.PublicIPSource)+", Medien UDP "+fmt.Sprint(s.MediaPort)+")"))
	push := yes(s.APNsConfigured, "eingerichtet", "nicht eingerichtet")
	if s.LastPush != nil {
		if s.LastPush.OK {
			push += mutedS.Render(" · letzter Push ok " + s.LastPush.At.Local().Format("15:04"))
		} else {
			push += failS.Render(" · letzter Push fehlgeschlagen")
		}
	}
	fmt.Fprintf(&b, "  Push (APNs)  %s\n", push)
	tr := mutedS.Render("● ") + "nicht eingerichtet"
	if s.FritzBoxConfigured {
		tr = yes(len(s.FritzBoxFeatures) == 2, "Telefonbuch & Anrufliste verfügbar", "Abruf fehlgeschlagen")
	}
	fmt.Fprintf(&b, "  TR-064       %s\n", tr)
	fmt.Fprintf(&b, "  Geräte       %d gekoppelt, %d online · %d Anruf(e) aktiv\n", s.DevicesTotal, s.DevicesOnline, s.ActiveCalls)
	fmt.Fprintf(&b, "  Adresse      %s\n", orDash(s.PublicURL))
	fmt.Fprintf(&b, "  Version      %s · läuft seit %s\n", s.Version, up)
	fmt.Fprintf(&b, "  Fingerabdr.  %s\n", s.Fingerprint)
	if m.showConfig {
		b.WriteString("\n" + boldS.Render("Konfiguration") + mutedS.Render(" (nur Anzeige, Secrets geschwärzt)") + "\n")
		b.WriteString(renderConfig(m.config, "  "))
	} else {
		b.WriteString(mutedS.Render("\n  k zeigt die wirksame Konfiguration.") + "\n")
	}
	return b.String()
}

func renderConfig(c map[string]any, indent string) string {
	var b strings.Builder
	for _, section := range []string{"bridge", "sip", "media", "apns", "fritzbox", "log"} {
		v, ok := c[section].(map[string]any)
		if !ok {
			continue
		}
		var parts []string
		for k, val := range v {
			parts = append(parts, fmt.Sprintf("%s=%v", k, val))
		}
		sortStrings(parts)
		b.WriteString(indent + selS.Render(section) + " " + strings.Join(parts, " ") + "\n")
	}
	return b.String()
}

func (m Model) viewDevices() string {
	if len(m.devices) == 0 {
		return "  Noch kein Gerät gekoppelt. Taste 3 → n erzeugt einen Kopplungscode.\n"
	}
	var b strings.Builder
	multi := m.multiProfile()
	header := fmt.Sprintf("   %-20s %-8s %-14s %-11s %s", "NAME", "PLATTF.", "MEDIEN", "PUSH", "ZULETZT")
	if multi {
		header = fmt.Sprintf("   %-20s %-12s %-8s %-14s %-11s %s", "NAME", "PROFIL", "PLATTF.", "MEDIEN", "PUSH", "ZULETZT")
	}
	b.WriteString(mutedS.Render(header) + "\n")
	for i, d := range m.devices {
		state := failS.Render("○")
		if d.Online {
			state = okS.Render("●")
		}
		seen := "–"
		if d.Online {
			seen = "online"
		} else if !d.LastSeen.IsZero() {
			seen = d.LastSeen.Local().Format("02.01. 15:04")
		}
		line := fmt.Sprintf("%s %-20s %-8s %-14s %-11s %s", state, trunc(d.Name, 20), trunc(d.Platform, 8), trunc(d.Media, 14), trunc(orDash(d.Push), 11), seen)
		if multi {
			line = fmt.Sprintf("%s %-20s %-12s %-8s %-14s %-11s %s", state, trunc(d.Name, 20), trunc(d.ProfileName, 12), trunc(d.Platform, 8), trunc(d.Media, 14), trunc(orDash(d.Push), 11), seen)
		}
		if i == m.cursor {
			line = selS.Render("›") + line
		} else {
			line = " " + line
		}
		b.WriteString(line + "\n")
	}
	if m.confirm != "" {
		name := ""
		for _, d := range m.devices {
			if d.ID == m.confirm {
				name = d.Name
			}
		}
		b.WriteString("\n" + warnS.Width(max(20, m.width-2)).PaddingLeft(2).Render(fmt.Sprintf("„%s“ entfernen? Verbindung wird sofort getrennt, gekoppelte Uhren ebenfalls entfernt. (j/n)", name)) + "\n")
	} else if len(m.devices) > 0 {
		d := m.devices[m.cursor]
		via := ""
		if d.PairedByName != "" {
			via = " über " + d.PairedByName
		}
		b.WriteString(mutedS.Width(max(20, m.width-2)).PaddingLeft(2).Render(fmt.Sprintf("\nID %s · Schlüssel %s · gekoppelt %s%s", d.ID, d.KeyFingerprint, d.CreatedAt.Local().Format("02.01.2006"), via)) + "\n")
	}
	return b.String()
}

func (m Model) hasLan(id string) bool {
	for _, r := range m.lan {
		if r.ID == id {
			return true
		}
	}
	return false
}

// viewLan lists the pairing requests from the home network.
func (m Model) viewLan() string {
	if len(m.lan) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(boldS.Render("Anfragen aus dem Heimnetz") + "\n")
	for i, r := range m.lan {
		marker := "  "
		if i == m.lanCursor {
			marker = selS.Render("› ")
		}
		fmt.Fprintf(&b, "%s%s  %s  %s %s · %s · bis %s\n", marker, selS.Render(admin.GroupSAS(r.SAS)), boldS.Render(r.DeviceName),
			mutedS.Render(orDash(r.Model)), r.IP, mutedS.Render("Schlüssel "+r.KeyFingerprint), r.ExpiresAt.Local().Format("15:04:05"))
	}
	if m.lanConfirm != "" {
		for _, r := range m.lan {
			if r.ID == m.lanConfirm {
				b.WriteString(warnS.Render(fmt.Sprintf("  Zeigt „%s“ genau den Code %s? j = freigeben, andere Taste = abbrechen", r.DeviceName, admin.GroupSAS(r.SAS))) + "\n")
			}
		}
	} else {
		b.WriteString(mutedS.Render("  Nur freigeben, wenn das iPhone denselben Code zeigt. a freigeben · d ablehnen · ↑↓ auswählen") + "\n")
	}
	return b.String() + "\n"
}

func (m Model) viewPairing() string {
	lan := m.viewLan()
	if m.pairing == nil {
		return lan + "  n  neuen Kopplungscode erzeugen (einmalig, 10 Minuten gültig).\n" +
			mutedS.Render("  Im Heimnetz geht es auch ohne Code: Housephone auf dem iPhone öffnen, die Bridge antippen und hier freigeben.") + "\n" +
			mutedS.Render("  Das iPhone scannt den QR-Code; die Apple Watch koppelt sich danach automatisch.") + "\n"
	}
	p := m.pairing
	var b strings.Builder
	b.WriteString(lan)
	if m.pairingDone != nil {
		switch {
		case m.pairingDone.Used && m.pairingDone.Device != nil:
			d := m.pairingDone.Device
			b.WriteString(okS.Render("✓ Gekoppelt: ") + boldS.Render(d.Name) + fmt.Sprintf(" (%s, %s)\n", d.Platform, orDash(d.Model)))
			b.WriteString(fmt.Sprintf("  Schlüssel %s · ID %s\n", d.KeyFingerprint, d.ID))
			b.WriteString(warnS.Render("  Warst du das nicht? Taste 2 → Gerät auswählen → x entfernt es sofort.") + "\n")
		case m.pairingDone.Used:
			b.WriteString(okS.Render("✓ Code wurde benutzt.") + "\n")
		default:
			b.WriteString(warnS.Render("Code abgelaufen oder widerrufen – kein Gerät gekoppelt.") + "\n")
		}
		b.WriteString(mutedS.Render("  n erzeugt einen neuen Code.") + "\n")
		return b.String()
	}
	qrLines := strings.Count(m.pairingQR, "\n") + 1
	if qrLines+8 <= m.height {
		b.WriteString(m.pairingQR + "\n")
	} else {
		b.WriteString(warnS.Render(fmt.Sprintf("  Fenster zu klein für den QR-Code (%d Zeilen nötig) – Terminal vergrößern oder Link verwenden.", qrLines+8)) + "\n")
	}
	fmt.Fprintf(&b, "  Code %s · gültig bis %s · Bridge %s\n", boldS.Render(p.Grouped), p.ExpiresAt.Local().Format("15:04"), trunc(p.Fingerprint, 12)+"…")
	if m.multiProfile() {
		fmt.Fprintf(&b, "  Profil %s\n", boldS.Render(p.ProfileName))
	}
	b.WriteString(mutedS.Render("  "+p.Link) + "\n")
	b.WriteString(selS.Render("  Warte auf das Gerät …") + mutedS.Render(" Esc widerruft den Code") + "\n")
	return b.String()
}

func (m Model) viewCalls() string {
	var b strings.Builder
	t, d := m.stats.Total, m.stats.Today
	fmt.Fprintf(&b, "  %s eingehend %d · angenommen %d · verpasst %d · ausgehend %d\n", boldS.Render("Heute "), d.Incoming, d.Answered, d.Missed, d.Outgoing)
	fmt.Fprintf(&b, "  %s eingehend %d · angenommen %d · verpasst %d · ausgehend %d\n", boldS.Render("Gesamt"), t.Incoming, t.Answered, t.Missed, t.Outgoing)
	fmt.Fprintf(&b, "  %s Push %d · Medien %d · Codecs %s\n", boldS.Render("Fehler"), t.PushFailures, t.MediaFailures, codecs(t.Codecs))
	if m.multiProfile() {
		for _, p := range m.status.Profiles {
			c := m.stats.ByProfile[p.ID]
			fmt.Fprintf(&b, "  %s eingehend %d · angenommen %d · verpasst %d · ausgehend %d\n", boldS.Render(fmt.Sprintf("%-6s", trunc(p.Name, 6))), c.Incoming, c.Answered, c.Missed, c.Outgoing)
		}
	}
	b.WriteString("\n" + boldS.Render("Aktiv") + "\n")
	if len(m.calls.Active) == 0 {
		b.WriteString(mutedS.Render("  keine") + "\n")
	}
	for _, c := range m.calls.Active {
		state := "klingelt"
		if !c.ConnectedAt.IsZero() {
			state = "verbunden " + m.now().Sub(c.ConnectedAt).Round(time.Second).String()
		}
		fmt.Fprintf(&b, "  %s %s %-8s %s %s%s\n", okS.Render("●"), arrow(c.Direction), c.Number, orDash(c.Codec), state, m.callProfile(c))
	}
	b.WriteString("\n" + boldS.Render("Zuletzt") + "\n")
	if len(m.calls.Recent) == 0 {
		b.WriteString(mutedS.Render("  noch keine Anrufe seit dem Start") + "\n")
	}
	room := max(3, m.height-15)
	for i, c := range m.calls.Recent {
		if i >= room {
			break
		}
		dur := "–"
		if !c.ConnectedAt.IsZero() {
			dur = c.EndedAt.Sub(c.ConnectedAt).Round(time.Second).String()
		}
		reason := c.Reason
		if c.SIPCode != 0 {
			reason += fmt.Sprintf(" (SIP %d)", c.SIPCode)
		}
		fmt.Fprintf(&b, "  %s %s %-8s %-6s %-6s %s%s\n", mutedS.Render(c.StartedAt.Local().Format("02.01. 15:04")), arrow(c.Direction), c.Number, orDash(c.Codec), dur, reason, m.callProfile(c))
	}
	return b.String()
}

// callProfile names the profile of a call when there are several.
func (m Model) callProfile(c admin.CallInfo) string {
	if !m.multiProfile() || c.Profile == "" {
		return ""
	}
	return mutedS.Render(" · " + m.profileName(c.Profile))
}

func (m Model) viewLogs() string {
	levels := []string{"alle", "INFO+", "WARN+", "ERROR"}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s Level: %s", mutedS.Render("l"), boldS.Render(levels[m.level]))
	if m.search != "" {
		fmt.Fprintf(&b, " · Suche: %s", boldS.Render(m.search))
	}
	b.WriteString("\n")
	var shown []admin.LogLine
	for _, l := range m.logs {
		if levelRank(l.Level) < m.level {
			continue
		}
		if m.search != "" && !strings.Contains(strings.ToLower(l.Text), strings.ToLower(m.search)) {
			continue
		}
		shown = append(shown, l)
	}
	room := max(3, m.height-8)
	if len(shown) > room {
		shown = shown[len(shown)-room:]
	}
	for _, l := range shown {
		lvl := mutedS.Render(fmt.Sprintf("%-5s", l.Level))
		switch levelRank(l.Level) {
		case 2:
			lvl = warnS.Render(fmt.Sprintf("%-5s", l.Level))
		case 3:
			lvl = failS.Render(fmt.Sprintf("%-5s", l.Level))
		}
		b.WriteString(fmt.Sprintf("%s %s %s\n", mutedS.Render(l.At.Local().Format("15:04:05")), lvl, trunc(l.Text, max(20, m.width-16))))
	}
	if len(shown) == 0 {
		b.WriteString(mutedS.Render("  keine passenden Zeilen") + "\n")
	}
	return b.String()
}

func (m Model) viewSelfTest() string {
	if m.checks == nil {
		return "  Prüfe …\n"
	}
	var b strings.Builder
	overall := map[admin.CheckState]string{admin.CheckOK: okS.Render("Alles bereit."), admin.CheckWarn: warnS.Render("Läuft – mit Hinweisen."), admin.CheckFail: failS.Render("Es fehlt noch etwas.")}[admin.Worst(m.checks)]
	b.WriteString("  " + overall + mutedS.Render("  (r prüft erneut)") + "\n\n")
	width := m.width
	if width <= 0 {
		width = 80
	}
	for i, c := range m.checks {
		mark := "  "
		if i == m.check {
			mark = selS.Render("▸ ")
		}
		fmt.Fprintf(&b, "%s%s %-24s %s\n", mark, dot(c.State), c.Name, trunc(c.Detail, max(10, width-30)))
	}
	if m.check < len(m.checks) {
		c := m.checks[m.check]
		hint := c.Hint
		if hint == "" || c.State == admin.CheckOK {
			hint = "Alles in Ordnung."
		}
		b.WriteString("\n" + lipgloss.NewStyle().Width(max(20, width-4)).PaddingLeft(2).Render(boldS.Render(c.Name+": ")+hint) + "\n")
	}
	return b.String()
}

func levelRank(l string) int {
	switch strings.ToUpper(l) {
	case "DEBUG":
		return 0
	case "INFO":
		return 1
	case "WARN":
		return 2
	case "ERROR":
		return 3
	}
	return 1
}

func arrow(dir string) string {
	if dir == "outgoing" {
		return "↗"
	}
	return "↙"
}

func codecs(c map[string]int) string {
	if len(c) == 0 {
		return "–"
	}
	var parts []string
	for k, v := range c {
		parts = append(parts, fmt.Sprintf("%s %d", k, v))
	}
	sortStrings(parts)
	return strings.Join(parts, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
