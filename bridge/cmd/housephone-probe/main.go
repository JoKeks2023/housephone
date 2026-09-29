// Command housephone-probe is a development tool: a minimal "phone" that
// connects to a bridge like the watch app does (websocket-pcma) and lets you
// test real calls through a real FRITZ!Box from a computer, without an
// iPhone, APNs or a tunnel.
//
// It speaks signaling v2 (HP2): a software P-256 key instead of the Secure
// Enclave, pairing over HTTPS with the bridge fingerprint from the pair
// command, signed requests and sealed frames.
//
//	housephone-probe -link 'housephone://pair?v=2&…'                                  # pair once (link from pair)
//	housephone-probe -url ws://127.0.0.1:8080/v1/ws -pair K7P2-XH9Q-RMW4-DZT8 -fp …   # or code + fingerprint
//	housephone-probe                                                                  # answer calls (echo)
//	housephone-probe -dial 0170123456                                                 # call out
//
// In echo mode the caller hears themselves; in tone mode a 425 Hz tone.
// Received audio levels are printed once per second.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

const frameBytes = 160 // 20 ms of A-law at 8 kHz

// credentials is what the probe keeps after pairing (file mode 0600). A
// real device keeps its key in the Secure Enclave; the probe stores the
// P-256 scalar, so the file must be treated like a password.
type credentials struct {
	URL               string `json:"url"`
	DeviceID          string `json:"deviceId"`
	BridgeID          string `json:"bridgeId"`
	BridgeName        string `json:"bridgeName"`
	BridgeFingerprint string `json:"bridgeFingerprint"`
	BridgePublicKey   string `json:"bridgePublicKey"`
	PrivateKey        string `json:"privateKey"`
}

func main() {
	url := flag.String("url", "ws://127.0.0.1:8080/v1/ws", "WebSocket URL of the bridge")
	link := flag.String("link", "", "pairing link housephone://pair?v=2&… (from `housephone-bridge pair`); pairs and saves credentials")
	pairCode := flag.String("pair", "", "pairing code (from `housephone-bridge pair`), with -fp and -url; pairs and saves credentials")
	fingerprint := flag.String("fp", "", "bridge fingerprint for -pair (`housephone-bridge identity`)")
	credsFile := flag.String("creds", "housephone-probe.json", "credentials file")
	mode := flag.String("mode", "echo", "audio sent to the other side: echo | tone | silence")
	dial := flag.String("dial", "", "call this number instead of waiting for incoming calls")
	answerAfter := flag.Duration("answer-after", 2*time.Second, "ring this long before answering incoming calls")
	record := flag.String("record", "", "write the received audio of the call to this WAV file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *link != "" || *pairCode != "" {
		target := hp2.PairingLink{URL: *url, Code: *pairCode, Fingerprint: *fingerprint}
		if *link != "" {
			parsed, err := hp2.ParsePairingLink(*link)
			if err != nil {
				fail("Kopplungslink: %v", err)
			}
			target = parsed
		} else if target.Fingerprint == "" {
			fail("-pair braucht -fp (Fingerabdruck aus `housephone-bridge pair` oder `identity`)")
		}
		creds, err := pair(ctx, target)
		if err != nil {
			fail("Kopplung fehlgeschlagen: %v", err)
		}
		if err := saveCredentials(*credsFile, creds); err != nil {
			fail("Zugangsdaten speichern: %v", err)
		}
		fmt.Printf("✓ Gekoppelt mit „%s“ (%s) als %s, gespeichert in %s\n", creds.BridgeName, creds.BridgeFingerprint, creds.DeviceID, *credsFile)
		return
	}

	creds, err := loadCredentials(*credsFile)
	if err != nil {
		fail("Zugangsdaten lesen (%v) – zuerst mit -pair koppeln", err)
	}
	if *url != "" && flag.Lookup("url").Value.String() != flag.Lookup("url").DefValue {
		creds.URL = *url
	}
	p := &probe{mode: *mode, answerAfter: *answerAfter, recordPath: *record}
	if err := p.run(ctx, creds, *dial); err != nil && !errors.Is(err, context.Canceled) {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "✗ "+format+"\n", args...)
	os.Exit(1)
}

// MARK: - Pairing

// pair creates a fresh key and pairs over HTTPS (POST /v1/pair); the
// bridge's answer must match the fingerprint.
func pair(ctx context.Context, link hp2.PairingLink) (credentials, error) {
	base, err := hp2.HTTPBase(link.URL)
	if err != nil {
		return credentials{}, err
	}
	key, err := hp2.NewSoftwareKey()
	if err != nil {
		return credentials{}, err
	}
	host, _ := os.Hostname()
	res, err := hp2.Pair(ctx, nil, base, key, link.Code, link.Fingerprint, "Probetelefon "+host, protocol.PlatformIOS, "housephone-probe")
	if err != nil {
		return credentials{}, err
	}
	scalar, err := key.Scalar()
	if err != nil {
		return credentials{}, err
	}
	return credentials{
		URL: link.URL, DeviceID: res.DeviceID, BridgeID: res.BridgeID, BridgeName: res.BridgeName,
		BridgeFingerprint: link.Fingerprint, BridgePublicKey: hp2.B64(res.BridgePub), PrivateKey: hp2.B64(scalar),
	}, nil
}

func saveCredentials(path string, c credentials) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadCredentials(path string) (credentials, error) {
	var c credentials
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.PrivateKey == "" || c.BridgePublicKey == "" {
		return c, errors.New("Zugangsdaten aus Signalisierung v1 – neu koppeln")
	}
	return c, nil
}

// client builds the HP2 client from the stored credentials and checks the
// pinned bridge key against the fingerprint.
func (c credentials) client() (*hp2.Client, error) {
	scalar, err := hp2.DecodeB64(c.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("privateKey: %w", err)
	}
	key, err := hp2.SoftwareKeyFromScalar(scalar)
	if err != nil {
		return nil, fmt.Errorf("privateKey: %w", err)
	}
	pub, err := hp2.DecodeB64(c.BridgePublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize || hp2.Fingerprint(pub) != c.BridgeFingerprint {
		return nil, errors.New("bridgePublicKey passt nicht zum Fingerabdruck – neu koppeln")
	}
	base, err := hp2.HTTPBase(c.URL)
	if err != nil {
		return nil, err
	}
	return &hp2.Client{BaseURL: base, DeviceID: c.DeviceID, BridgeID: c.BridgeID, Key: key, BridgePub: pub}, nil
}

// MARK: - Calls

type probe struct {
	mode        string
	answerAfter time.Duration
	recordPath  string

	conn *hp2.Conn

	mu       sync.Mutex
	callID   string
	incoming bool
	audioOn  bool
	stopTone context.CancelFunc

	frames   int
	sumSq    float64
	samples  int
	recorded []int16
}

func (p *probe) run(ctx context.Context, creds credentials, dial string) error {
	client, err := creds.client()
	if err != nil {
		return err
	}
	conn, err := client.Dial(ctx)
	if err != nil {
		var se *hp2.StatusError
		if errors.As(err, &se) && se.Status == http.StatusUnauthorized {
			return errors.New("Bridge lehnt die Anmeldung ab (401) – Uhrzeit prüfen oder neu koppeln")
		}
		return err
	}
	defer conn.WS().CloseNow()
	conn.WS().SetReadLimit(1 << 20)
	p.conn = conn

	if err := p.send(ctx, protocol.TypeHello, protocol.Hello{
		AppVersion: "probe", Platform: protocol.PlatformIOS,
		MediaCapabilities: []string{protocol.MediaWebSocketPCMA},
	}); err != nil {
		return err
	}

	go p.printLevels(ctx)
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			p.hangupOnExit()
			if errors.Is(err, hp2.ErrIntegrity) {
				return errors.New("Frame der Bridge nicht authentisch – Verbindung getrennt")
			}
			return err
		}
		if kind == hp2.FrameAudio {
			p.receiveAudio(ctx, data)
			continue
		}
		if kind != hp2.FrameJSON {
			continue
		}
		env, err := protocol.ParseEnvelope(data[1:])
		if err != nil {
			continue
		}
		done, err := p.handle(ctx, env, dial)
		if err != nil || done {
			return err
		}
	}
}

func (p *probe) handle(ctx context.Context, env protocol.Envelope, dial string) (done bool, err error) {
	switch env.Type {
	case protocol.TypeWelcome:
		var w protocol.Welcome
		_ = env.Decode(&w)
		fmt.Printf("✓ Verbunden mit „%s“ (Bridge %s), FRITZ!Box angemeldet: %v, Funktionen: %v\n", w.BridgeName, w.BridgeVersion, w.SIPRegistered, w.Features)
		if dial != "" {
			id := newUUID()
			p.setCall(id, false)
			fmt.Printf("→ Wähle %s …\n", dial)
			return false, p.send(ctx, protocol.TypeCallDial, protocol.CallDial{CallID: id, Number: dial})
		}
		fmt.Printf("… warte auf Anrufe (Modus: %s, annehmen nach %v). Strg-C beendet.\n", p.mode, p.answerAfter)
	case protocol.TypeStatus:
		var s protocol.Status
		_ = env.Decode(&s)
		fmt.Printf("• FRITZ!Box angemeldet: %v\n", s.SIPRegistered)
	case protocol.TypeCallIncoming:
		var in protocol.CallIncoming
		_ = env.Decode(&in)
		if p.currentCall() != "" && p.currentCall() != in.CallID {
			return false, nil
		}
		if p.currentCall() == "" {
			who := in.Caller
			if who == "" {
				who = "unterdrückte Nummer"
			}
			if in.CallerName != "" {
				who = in.CallerName + " (" + in.Caller + ")"
			}
			fmt.Printf("📞 Eingehender Anruf von %s\n", who)
			p.setCall(in.CallID, true)
			return false, p.send(ctx, protocol.TypeCallAttach, protocol.CallAttach{CallID: in.CallID})
		}
	case protocol.TypeCallMedia:
		var m protocol.CallMedia
		_ = env.Decode(&m)
		if m.CallID != p.currentCall() {
			return false, nil
		}
		fmt.Printf("• Medienweg: %s, %s, %d Hz, %d ms\n", m.Transport, m.Codec, m.SampleRate, m.FrameMs)
		if p.isIncoming() {
			go p.answerLater(ctx, m.CallID)
		}
	case protocol.TypeCallState:
		var s protocol.CallState
		_ = env.Decode(&s)
		if s.CallID != p.currentCall() {
			return false, nil
		}
		fmt.Printf("• Status: %s\n", s.State)
		if s.State == protocol.CallStateConnected || s.State == protocol.CallStateEarlyMedia {
			p.startAudio(ctx)
		}
	case protocol.TypeCallEnded:
		var e protocol.CallEnded
		_ = env.Decode(&e)
		if e.CallID != p.currentCall() {
			return false, nil
		}
		sip := ""
		if e.SIPCode != 0 {
			sip = fmt.Sprintf(" (SIP %d)", e.SIPCode)
		}
		fmt.Printf("✖ Anruf beendet: %s%s – %d Rahmen empfangen\n", e.Reason, sip, p.receivedFrames())
		p.endCall()
		return dial != "", nil
	case protocol.TypeError:
		var e protocol.Error
		_ = env.Decode(&e)
		fmt.Printf("⚠ Fehler von der Bridge: %s – %s\n", e.Code, e.Message)
	}
	return false, nil
}

func (p *probe) answerLater(ctx context.Context, callID string) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(p.answerAfter):
	}
	if p.currentCall() != callID {
		return
	}
	fmt.Println("▶ Nehme an")
	if err := p.send(ctx, protocol.TypeCallAccept, protocol.CallAccept{CallID: callID}); err != nil {
		fmt.Printf("⚠ Annehmen fehlgeschlagen: %v\n", err)
		return
	}
	p.startAudio(ctx)
}

// MARK: - Audio

func (p *probe) startAudio(ctx context.Context) {
	p.mu.Lock()
	if p.audioOn {
		p.mu.Unlock()
		return
	}
	p.audioOn = true
	if p.mode != "tone" && p.mode != "silence" {
		p.mu.Unlock()
		return // echo answers each received frame
	}
	toneCtx, cancel := context.WithCancel(ctx)
	p.stopTone = cancel
	p.mu.Unlock()
	go p.sendTone(toneCtx)
}

func (p *probe) sendTone(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var n int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		frame := make([]byte, 1+frameBytes)
		frame[0] = protocol.AudioFrameType
		for i := range frameBytes {
			sample := 0.0
			if p.mode == "tone" {
				sample = 0.3 * math.Sin(2*math.Pi*425*float64(n)/8000)
			}
			frame[1+i] = alawEncode(int16(sample * 32767))
			n++
		}
		if err := p.writeAudio(ctx, frame); err != nil {
			return
		}
	}
}

func (p *probe) receiveAudio(ctx context.Context, data []byte) {
	if len(data) != 1+frameBytes || data[0] != protocol.AudioFrameType {
		return
	}
	p.mu.Lock()
	p.frames++
	for _, b := range data[1:] {
		s := alawDecode(b)
		p.sumSq += float64(s) * float64(s)
		p.samples++
		if p.recordPath != "" {
			p.recorded = append(p.recorded, s)
		}
	}
	echo := p.audioOn && p.mode == "echo"
	p.mu.Unlock()
	if echo {
		_ = p.writeAudio(ctx, data)
	}
}

func (p *probe) printLevels(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		p.mu.Lock()
		frames, sumSq, samples := p.frames, p.sumSq, p.samples
		p.sumSq, p.samples = 0, 0
		active := p.callID != ""
		p.mu.Unlock()
		if !active || samples == 0 {
			continue
		}
		rms := math.Sqrt(sumSq/float64(samples)) / 32768
		level := "Stille"
		if rms > 0 {
			level = fmt.Sprintf("%.0f dBFS", 20*math.Log10(rms))
		}
		fmt.Printf("🔊 empfangen: %d Rahmen gesamt, Pegel %s\n", frames, level)
	}
}

// MARK: - State

func (p *probe) setCall(id string, incoming bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callID, p.incoming, p.audioOn, p.frames, p.recorded = id, incoming, false, 0, nil
}

func (p *probe) currentCall() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.callID
}

func (p *probe) isIncoming() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.incoming
}

func (p *probe) receivedFrames() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frames
}

func (p *probe) endCall() {
	p.mu.Lock()
	if p.stopTone != nil {
		p.stopTone()
		p.stopTone = nil
	}
	recorded := p.recorded
	p.callID, p.audioOn, p.recorded = "", false, nil
	p.mu.Unlock()
	if p.recordPath != "" && len(recorded) > 0 {
		if err := writeWAV(p.recordPath, recorded); err != nil {
			fmt.Printf("⚠ Aufnahme speichern: %v\n", err)
		} else {
			fmt.Printf("• Aufnahme: %s (%.1f s)\n", p.recordPath, float64(len(recorded))/8000)
		}
	}
}

func (p *probe) hangupOnExit() {
	id := p.currentCall()
	if id == "" || p.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.send(ctx, protocol.TypeCallHangup, protocol.CallHangup{CallID: id, Reason: protocol.HangupReasonHangup})
	fmt.Println("✖ Aufgelegt")
}

// MARK: - Wire helpers

func (p *probe) send(ctx context.Context, msgType string, payload any) error {
	env, err := protocol.NewEnvelope(msgType, payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return p.conn.WriteJSON(writeCtx, data)
}

// writeAudio sends a sealed audio frame (type byte 0x01 + 160 bytes A-law).
func (p *probe) writeAudio(ctx context.Context, frame []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return p.conn.WriteAudio(writeCtx, frame)
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// MARK: - G.711 A-law and WAV

func alawEncode(pcm int16) byte {
	sign := byte(0x80)
	s := int(pcm)
	if s < 0 {
		sign = 0
		s = -s - 1
	}
	if s > 32635 {
		s = 32635
	}
	var out byte
	if s >= 256 {
		exp := 7
		for mask := 0x4000; s&mask == 0 && exp > 1; mask >>= 1 {
			exp--
		}
		mantissa := (s >> (exp + 3)) & 0x0f
		out = byte(exp<<4 | mantissa)
	} else {
		out = byte(s >> 4)
	}
	return (out | sign) ^ 0x55
}

func alawDecode(a byte) int16 {
	a ^= 0x55
	t := int(a&0x0f) << 4
	seg := int(a&0x70) >> 4
	switch seg {
	case 0:
		t += 8
	case 1:
		t += 0x108
	default:
		t += 0x108
		t <<= seg - 1
	}
	if a&0x80 != 0 {
		return int16(t)
	}
	return int16(-t)
}

func writeWAV(path string, samples []int16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dataLen := uint32(len(samples) * 2)
	header := []any{
		[4]byte{'R', 'I', 'F', 'F'}, 36 + dataLen, [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, dataLen,
	}
	for _, v := range header {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	return binary.Write(f, binary.LittleEndian, samples)
}
