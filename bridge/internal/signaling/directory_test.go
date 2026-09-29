package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

type fakeDirectory struct {
	mu        sync.Mutex
	body      []byte
	etag      string
	history   []byte
	err       error
	features  []string
	lastLimit int
}

type messageError struct{ msg string }

func (e messageError) Error() string       { return "fritzbox: " + e.msg }
func (e messageError) UserMessage() string { return e.msg }

func (d *fakeDirectory) Phonebook(context.Context) ([]byte, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.body, d.etag, d.err
}

func (d *fakeDirectory) History(_ context.Context, limit int) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastLimit = limit
	return d.history, d.err
}

func (d *fakeDirectory) Features() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.features
}

func withDirectory(t *testing.T, dir Directory) (*testServer, *testDevice) {
	t.Helper()
	ts := newTestServer(t)
	ts.srv.cfg.Directory = dir
	return ts, ts.pairDevice(t)
}

func TestPhonebookAndHistoryNeedAuthAndConfiguration(t *testing.T) {
	ts, d := withDirectory(t, nil)
	for _, path := range []string{"/v1/phonebook", "/v1/history"} {
		if res, _ := ts.request(t, http.MethodGet, path, nil, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without auth: %d", path, res.StatusCode)
		}
		res := d.do(t, http.MethodGet, path, nil)
		if res.Status != http.StatusServiceUnavailable {
			t.Fatalf("%s not configured: %d %s", path, res.Status, res.Body)
		}
		e := decodeError(t, res.Body)
		if e.Code != protocol.ErrorFritzBoxUnavailable || !strings.Contains(e.Message, "fritzbox.username") {
			t.Errorf("%s: %+v", path, e)
		}
	}
}

// The phonebook and the call history travel sealed: the raw answer does
// not contain the plaintext, and only the requesting device can open it.
func TestPhonebookAndHistoryAreSealed(t *testing.T) {
	dir := &fakeDirectory{
		body:    []byte(`{"updatedAt":"2026-09-29T18:04:05Z","contacts":[{"name":"Oma Erika"}]}`),
		history: []byte(`{"updatedAt":"2026-09-29T18:04:05Z","calls":[{"name":"Oma Erika"}]}`),
	}
	ts, d := withDirectory(t, dir)
	for path, want := range map[string][]byte{"/v1/phonebook": dir.body, "/v1/history": dir.history} {
		creq, err := hp2.NewClientRequest(d.Key, d.ID, testBridgeID, http.MethodGet, path, nil, nowForTest())
		if err != nil {
			t.Fatal(err)
		}
		res, raw := ts.request(t, http.MethodGet, path, http.Header{"Authorization": {creq.Authorization()}}, nil)
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != hp2.SealedContentType || strings.Contains(string(raw), "Erika") {
			t.Fatalf("%s: %d %s, plaintext visible: %v", path, res.StatusCode, res.Header.Get("Content-Type"), strings.Contains(string(raw), "Erika"))
		}
		keys, err := creq.VerifyAnswer(ts.identity.PublicKey(), res.StatusCode, raw, res.Header.Get(hp2.BridgeHeader))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := hp2.OpenBody(keys.BridgeToDevice, raw)
		if err != nil || string(plain) != string(want) {
			t.Fatalf("%s: opened %q %v", path, plain, err)
		}
		// Another device's session keys cannot open it.
		other := ts.pairWith(t, mustCode(t, ts), protocol.PlatformIOS, "iPad", "")
		oreq, _ := hp2.NewClientRequest(other.Key, other.ID, testBridgeID, http.MethodGet, path, nil, nowForTest())
		ores, oraw := ts.request(t, http.MethodGet, path, http.Header{"Authorization": {oreq.Authorization()}}, nil)
		okeys, err := oreq.VerifyAnswer(ts.identity.PublicKey(), ores.StatusCode, oraw, ores.Header.Get(hp2.BridgeHeader))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := hp2.OpenBody(okeys.BridgeToDevice, raw); err == nil {
			t.Fatalf("%s: another session's key opened the answer", path)
		}
		// A body swapped between two answers fails the bridge signature.
		if _, err := creq.VerifyAnswer(ts.identity.PublicKey(), res.StatusCode, oraw, res.Header.Get(hp2.BridgeHeader)); err == nil {
			t.Fatalf("%s: swapped body accepted", path)
		}
	}
}

func TestPhonebookETag(t *testing.T) {
	dir := &fakeDirectory{body: []byte(`{"updatedAt":"2026-09-29T18:04:05Z","contacts":[]}`), etag: `"abc123"`}
	_, d := withDirectory(t, dir)

	res := d.do(t, http.MethodGet, "/v1/phonebook", nil)
	if res.Status != http.StatusOK || string(res.Body) != string(dir.body) || res.Header.Get("ETag") != `"abc123"` {
		t.Fatalf("GET: %d %q etag %q", res.Status, res.Body, res.Header.Get("ETag"))
	}
	for header, want := range map[string]int{
		`"abc123"`:               http.StatusNotModified,
		`W/"abc123"`:             http.StatusNotModified,
		`"old", "abc123"`:        http.StatusNotModified,
		`*`:                      http.StatusNotModified,
		`"old"`:                  http.StatusOK,
		`abc123`:                 http.StatusOK, // unquoted is not the same tag
		`"abc123-but-different"`: http.StatusOK,
	} {
		res, err := d.tryDo(http.MethodGet, "/v1/phonebook", nil, http.Header{"If-None-Match": {header}})
		if err != nil {
			t.Fatalf("If-None-Match %s: %v", header, err)
		}
		if res.Status != want {
			t.Errorf("If-None-Match %s: %d, want %d", header, res.Status, want)
		}
		if want == http.StatusNotModified && len(res.Body) != 0 {
			t.Errorf("304 with body %q", res.Body)
		}
	}
}

func TestHistoryLimit(t *testing.T) {
	dir := &fakeDirectory{history: []byte(`{"updatedAt":"2026-09-29T18:04:05Z","calls":[]}`)}
	_, d := withDirectory(t, dir)
	for query, want := range map[string]int{"": 100, "?limit=1": 1, "?limit=500": 500, "?limit=25": 25} {
		res := d.do(t, http.MethodGet, "/v1/history"+query, nil)
		dir.mu.Lock()
		got := dir.lastLimit
		dir.mu.Unlock()
		if res.Status != http.StatusOK || got != want || string(res.Body) != string(dir.history) {
			t.Errorf("%q: status %d, limit %d, want %d", query, res.Status, got, want)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=501", "?limit=abc", "?limit=-3"} {
		res := d.do(t, http.MethodGet, "/v1/history"+query, nil)
		if res.Status != http.StatusBadRequest || decodeError(t, res.Body).Code != protocol.ErrorBadRequest {
			t.Errorf("%q: %d %s", query, res.Status, res.Body)
		}
	}
}

func TestFritzBoxFailureIs503WithExplanation(t *testing.T) {
	dir := &fakeDirectory{err: messageError{"Die FRITZ!Box hat die Anmeldung abgelehnt."}}
	_, d := withDirectory(t, dir)
	for _, path := range []string{"/v1/phonebook", "/v1/history"} {
		res := d.do(t, http.MethodGet, path, nil)
		e := decodeError(t, res.Body)
		if res.Status != http.StatusServiceUnavailable || e.Code != protocol.ErrorFritzBoxUnavailable || e.Message != "Die FRITZ!Box hat die Anmeldung abgelehnt." {
			t.Errorf("%s: %d %+v", path, res.Status, e)
		}
	}
	dir.mu.Lock()
	dir.err = errors.New("plain error")
	dir.mu.Unlock()
	res := d.do(t, http.MethodGet, "/v1/phonebook", nil)
	if res.Status != http.StatusServiceUnavailable || decodeError(t, res.Body).Message == "" {
		t.Fatalf("plain error: %d %s", res.Status, res.Body)
	}
}

func TestWelcomeAnnouncesFeatures(t *testing.T) {
	for _, tc := range []struct {
		name string
		dir  Directory
		want []string
	}{
		{"not configured", nil, nil},
		{"configured, nothing loaded yet", &fakeDirectory{}, nil},
		{"both", &fakeDirectory{features: []string{protocol.FeatureFritzBoxPhonebook, protocol.FeatureFritzBoxHistory}}, []string{"fritzbox.phonebook", "fritzbox.history"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, d := withDirectory(t, tc.dir)
			c, err := ts.dial(t, d)
			if err != nil {
				t.Fatal(err)
			}
			defer c.WS().CloseNow()
			send(t, c, protocol.TypeHello, iPhoneHello)
			var raw json.RawMessage
			receive(t, c, protocol.TypeWelcome, &raw)
			var welcome protocol.Welcome
			if err := json.Unmarshal(raw, &welcome); err != nil {
				t.Fatal(err)
			}
			if strings.Join(welcome.Features, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("features %v, want %v", welcome.Features, tc.want)
			}
			if tc.want == nil && strings.Contains(string(raw), "features") {
				t.Fatalf("features must be omitted when empty: %s", raw)
			}
		})
	}
}
