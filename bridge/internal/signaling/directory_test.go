package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

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

func withDirectory(t *testing.T, dir Directory) (*testServer, protocol.PairOK) {
	t.Helper()
	ts := newTestServer(t)
	ts.srv.cfg.Directory = dir
	return ts, ts.pairDevice(t)
}

func TestPhonebookAndHistoryNeedAuthAndConfiguration(t *testing.T) {
	ts, ok := withDirectory(t, nil)
	for _, path := range []string{"/v1/phonebook", "/v1/history"} {
		if res, _ := ts.request(t, http.MethodGet, path, nil, nil); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without auth: %d", path, res.StatusCode)
		}
		res, body := ts.request(t, http.MethodGet, path, bearer(ok), nil)
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s not configured: %d %s", path, res.StatusCode, body)
		}
		e := decodeError(t, body)
		if e.Code != protocol.ErrorFritzBoxUnavailable || !strings.Contains(e.Message, "fritzbox.username") {
			t.Errorf("%s: %+v", path, e)
		}
	}
}

func TestPhonebookETag(t *testing.T) {
	dir := &fakeDirectory{body: []byte(`{"updatedAt":"2026-09-29T18:04:05Z","contacts":[]}`), etag: `"abc123"`}
	ts, ok := withDirectory(t, dir)

	res, body := ts.request(t, http.MethodGet, "/v1/phonebook", bearer(ok), nil)
	if res.StatusCode != http.StatusOK || string(body) != string(dir.body) || res.Header.Get("ETag") != `"abc123"` {
		t.Fatalf("GET: %d %q etag %q", res.StatusCode, body, res.Header.Get("ETag"))
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
		h := bearer(ok)
		h.Set("If-None-Match", header)
		res, body := ts.request(t, http.MethodGet, "/v1/phonebook", h, nil)
		if res.StatusCode != want {
			t.Errorf("If-None-Match %s: %d, want %d", header, res.StatusCode, want)
		}
		if want == http.StatusNotModified && len(body) != 0 {
			t.Errorf("304 with body %q", body)
		}
	}
}

func TestHistoryLimit(t *testing.T) {
	dir := &fakeDirectory{history: []byte(`{"updatedAt":"2026-09-29T18:04:05Z","calls":[]}`)}
	ts, ok := withDirectory(t, dir)
	for query, want := range map[string]int{"": 100, "?limit=1": 1, "?limit=500": 500, "?limit=25": 25} {
		res, body := ts.request(t, http.MethodGet, "/v1/history"+query, bearer(ok), nil)
		dir.mu.Lock()
		got := dir.lastLimit
		dir.mu.Unlock()
		if res.StatusCode != http.StatusOK || got != want || string(body) != string(dir.history) {
			t.Errorf("%q: status %d, limit %d, want %d", query, res.StatusCode, got, want)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=501", "?limit=abc", "?limit=-3"} {
		res, body := ts.request(t, http.MethodGet, "/v1/history"+query, bearer(ok), nil)
		if res.StatusCode != http.StatusBadRequest || decodeError(t, body).Code != protocol.ErrorBadRequest {
			t.Errorf("%q: %d %s", query, res.StatusCode, body)
		}
	}
}

func TestFritzBoxFailureIs503WithExplanation(t *testing.T) {
	dir := &fakeDirectory{err: messageError{"Die FRITZ!Box hat die Anmeldung abgelehnt."}}
	ts, ok := withDirectory(t, dir)
	for _, path := range []string{"/v1/phonebook", "/v1/history"} {
		res, body := ts.request(t, http.MethodGet, path, bearer(ok), nil)
		e := decodeError(t, body)
		if res.StatusCode != http.StatusServiceUnavailable || e.Code != protocol.ErrorFritzBoxUnavailable || e.Message != "Die FRITZ!Box hat die Anmeldung abgelehnt." {
			t.Errorf("%s: %d %+v", path, res.StatusCode, e)
		}
	}
	dir.mu.Lock()
	dir.err = errors.New("plain error")
	dir.mu.Unlock()
	res, body := ts.request(t, http.MethodGet, "/v1/phonebook", bearer(ok), nil)
	if res.StatusCode != http.StatusServiceUnavailable || decodeError(t, body).Message == "" {
		t.Fatalf("plain error: %d %s", res.StatusCode, body)
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
			ts, ok := withDirectory(t, tc.dir)
			c, _, err := ts.dial(t, bearer(ok))
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseNow()
			send(t, c, protocol.TypeHello, protocol.Hello{AppVersion: "1", Platform: protocol.PlatformIOS})
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
