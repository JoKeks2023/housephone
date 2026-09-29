package fritzbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/fritzbox/fritzboxtest"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

const httpFixtures = "../../../docs/protocol/fixtures/http/"

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// sameJSONExceptUpdatedAt compares a response with a fixture semantically,
// ignoring updatedAt (the fetch time).
func sameJSONExceptUpdatedAt(t *testing.T, got []byte, fixture string) {
	t.Helper()
	want, err := os.ReadFile(httpFixtures + fixture)
	if err != nil {
		t.Fatal(err)
	}
	var g, w map[string]any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	if _, ok := g["updatedAt"].(string); !ok {
		t.Errorf("updatedAt missing: %s", got)
	}
	delete(g, "updatedAt")
	delete(w, "updatedAt")
	if !reflect.DeepEqual(g, w) {
		gotPretty, _ := json.MarshalIndent(g, "", "  ")
		wantPretty, _ := json.MarshalIndent(w, "", "  ")
		t.Fatalf("response differs from %s\n got: %s\nwant: %s", fixture, gotPretty, wantPretty)
	}
}

func newClient(box *fritzboxtest.Box, password string) *Client {
	return NewClient(ClientConfig{Host: box.Host(), PlainPort: box.PlainPort(), Username: fritzboxtest.Username, Password: password, Logger: discard()})
}

func newTestDirectory(t *testing.T, src Source) *Directory {
	t.Helper()
	return NewDirectory(DirectoryConfig{Source: src, Location: berlin(t), CountryCode: "49", Logger: discard()})
}

func TestPhonebookFromFritzBoxMatchesFixture(t *testing.T) {
	box := fritzboxtest.Start(t)
	dir := newTestDirectory(t, newClient(box, fritzboxtest.Password))

	body, etag, err := dir.Phonebook(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sameJSONExceptUpdatedAt(t, body, "phonebook.json")
	if etag == "" || etag[0] != '"' {
		t.Fatalf("ETag %q is not a quoted strong tag", etag)
	}
	// Both phonebooks were downloaded from the fake: the "fritz.box" host
	// in the returned URLs was rewritten to the configured host.
	if n := box.PhonebookFetches.Load(); n != 2 {
		t.Fatalf("phonebook downloads = %d, want 2", n)
	}
}

func TestHistoryFromFritzBoxMatchesFixture(t *testing.T) {
	box := fritzboxtest.Start(t)
	dir := newTestDirectory(t, newClient(box, fritzboxtest.Password))

	body, err := dir.History(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	sameJSONExceptUpdatedAt(t, body, "history.json")
	lastMax := box.LastMax()
	if lastMax != "500" {
		t.Fatalf("call list requested with max=%q, want 500 (cached, sliced per request)", lastMax)
	}

	limited, err := dir.History(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	var h protocol.History
	if err := json.Unmarshal(limited, &h); err != nil {
		t.Fatal(err)
	}
	if len(h.Calls) != 2 || h.Calls[0].ID != "2513" {
		t.Fatalf("limit 2: %+v", h.Calls)
	}
	if n := box.CallListFetches.Load(); n != 1 {
		t.Fatalf("call list downloads = %d, want 1 (30 s cache)", n)
	}
}

func TestCallListTimesInSummerAndWinter(t *testing.T) {
	loc := berlin(t)
	xml := []byte(`<root>
<Call><Id>1</Id><Type>3</Type><Called>1</Called><Date>15.01.26 12:00</Date><Duration>0:01</Duration></Call>
<Call><Id>2</Id><Type>3</Type><Called>1</Called><Date>15.07.26 12:00</Date><Duration>0:01</Duration></Call>
<Call><Id>3</Id><Type>3</Type><Called>1</Called><Date>29.03.26 03:30</Date><Duration>0:01</Duration></Call>
<Call><Id>4</Id><Type>3</Type><Called>1</Called><Date>25.10.26 01:59</Date><Duration>0:01</Duration></Call>
</root>`)
	calls, err := parseCallList(xml, loc)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range calls {
		got[c.ID] = c.StartedAt.Format(time.RFC3339)
	}
	want := map[string]string{
		"1": "2026-01-15T11:00:00Z", // CET, UTC+1
		"2": "2026-07-15T10:00:00Z", // CEST, UTC+2
		"3": "2026-03-29T01:30:00Z", // first hour after the switch to summer time
		"4": "2026-10-24T23:59:00Z", // still summer time
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestCallListTypesPortsAndDurations(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><root>
<Call><Id>10</Id><Type>9</Type><Caller>0301</Caller><Port>10</Port><Date>01.09.26 10:00</Date><Duration>0:00</Duration></Call>
<Call><Id>11</Id><Type>10</Type><Caller>0302</Caller><Port>-1</Port><Date>01.09.26 10:01</Date><Duration>0:00</Duration></Call>
<Call><Id>12</Id><Type>11</Type><Called>0303</Called><Port>620</Port><Date>01.09.26 10:02</Date><Duration>1:05</Duration></Call>
<Call><Id>13</Id><Type>1</Type><Caller>0304</Caller><Port>6</Port><Date>01.09.26 10:03</Date><Duration>0:01</Duration></Call>
<Call><Id>14</Id><Type>1</Type><Caller>0305</Caller><Port>45</Port><Date>01.09.26 10:04</Date><Duration>0:01</Duration></Call>
<Call><Id>15</Id><Type>1</Type><Caller>0306</Caller><Port>50</Port><Date>01.09.26 10:05</Date><Duration>x</Duration></Call>
<Call><Id>16</Id><Type>7</Type><Caller>0307</Caller><Date>01.09.26 10:06</Date></Call>
<Call><Id>17</Id><Type>1</Type><Caller>0308</Caller><Date>kaputt</Date></Call>
</root>`)
	calls, err := parseCallList(xml, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ id, dir, res, by string }
	var got []row
	durations := map[string]int{}
	for _, c := range calls {
		got = append(got, row{c.ID, c.Direction, c.Result, c.AnsweredBy})
		durations[c.ID] = c.DurationSeconds
	}
	want := []row{
		{"15", "incoming", "answered", "phone"},
		{"14", "incoming", "answered", "answering_machine"},
		{"13", "incoming", "answered", "answering_machine"},
		{"12", "outgoing", "active", ""},
		{"11", "incoming", "rejected", ""},
		{"10", "incoming", "active", ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v (unknown type 7 and invalid date must be dropped)", got, want)
	}
	if durations["12"] != 3900 || durations["15"] != 0 {
		t.Fatalf("durations: %v", durations)
	}
}

func TestPhonebookInLatin1(t *testing.T) {
	data := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><phonebooks><phonebook name=\"Gesch\xe4ftlich\">" +
		"<contact><category>0</category><person><realName>M\xfcller</realName></person>" +
		"<telephony><number type=\"work\" prio=\"1\">089-1234 56</number><number type=\"pager\" prio=\"0\">0891</number></telephony>" +
		"<uniqueid>5</uniqueid></contact></phonebook></phonebooks>")
	contacts, err := parsePhonebook(data, "1", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.Contact{{
		ID: "1-5", Name: "Müller", Phonebook: "Geschäftlich",
		Numbers: []protocol.ContactNumber{
			{Number: "089123456", Type: "work", Preferred: true},
			{Number: "0891", Type: "other"},
		},
	}}
	if !reflect.DeepEqual(contacts, want) {
		t.Fatalf("got %+v\nwant %+v", contacts, want)
	}
}

func TestWrongPasswordIsReportedAsLoginProblem(t *testing.T) {
	box := fritzboxtest.Start(t)
	dir := newTestDirectory(t, newClient(box, "falsch"))
	_, _, err := dir.Phonebook(context.Background())
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindAuth {
		t.Fatalf("want KindAuth, got %v", err)
	}
	if fe.UserMessage() == "" || box.PhonebookFetches.Load() != 0 {
		t.Fatalf("message %q, downloads %d", fe.UserMessage(), box.PhonebookFetches.Load())
	}
	if len(dir.Features()) != 0 {
		t.Fatalf("features after failure: %v", dir.Features())
	}
}

func TestUPnPFaults(t *testing.T) {
	for code, kind := range map[string]ErrorKind{"606": KindAuth, "820": KindUnsupported, "713": KindProtocol} {
		box := fritzboxtest.Start(t)
		box.SetFault(code)
		_, err := newClient(box, fritzboxtest.Password).Phonebook(context.Background())
		var fe *Error
		if !errors.As(err, &fe) || fe.Kind != kind {
			t.Errorf("fault %s: want kind %d, got %v", code, kind, err)
		}
	}
}

func TestCallListSwitchedOff(t *testing.T) {
	box := fritzboxtest.Start(t)
	box.SetCallListOff(true)
	_, err := newClient(box, fritzboxtest.Password).History(context.Background(), 10, time.UTC)
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindUnsupported {
		t.Fatalf("want KindUnsupported, got %v", err)
	}
}

func TestFritzBoxUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(l.Addr().String())
	l.Close()
	client := NewClient(ClientConfig{Host: "127.0.0.1", PlainPort: port, Username: "u", Password: "p", Timeout: time.Second, Logger: discard()})
	_, err = client.Phonebook(context.Background())
	var fe *Error
	if !errors.As(err, &fe) || fe.Kind != KindUnreachable {
		t.Fatalf("want KindUnreachable, got %v", err)
	}
}

func TestSecurityPortIsAskedAgainAfterConnectionFailure(t *testing.T) {
	box := fritzboxtest.Start(t)
	client := newClient(box, fritzboxtest.Password)
	if _, err := client.Phonebook(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The FRITZ!Box restarts with a new TLS port.
	box.RestartTLS()
	if _, err := client.Phonebook(context.Background()); err == nil {
		t.Fatal("expected the first call against the old port to fail")
	}
	if _, err := client.Phonebook(context.Background()); err != nil {
		t.Fatalf("second call should rediscover the port: %v", err)
	}
}

// countingSource is a Source with controllable results.
type countingSource struct {
	mu            sync.Mutex
	contacts      []protocol.Contact
	calls         []protocol.HistoryCall
	phonebookErr  error
	historyErr    error
	phonebookHits atomic.Int32
	historyHits   atomic.Int32
	gate          chan struct{}
}

func (s *countingSource) Phonebook(ctx context.Context) ([]protocol.Contact, error) {
	s.phonebookHits.Add(1)
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contacts, s.phonebookErr
}

func (s *countingSource) History(ctx context.Context, max int, loc *time.Location) ([]protocol.HistoryCall, error) {
	s.historyHits.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.historyErr
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func oma() []protocol.Contact {
	return []protocol.Contact{{ID: "0-1", Name: "Oma", Phonebook: "Telefonbuch", Numbers: []protocol.ContactNumber{{Number: "030123456", Type: "home", Preferred: true}}}}
}

func TestDirectoryCachesAndCollapsesConcurrentRequests(t *testing.T) {
	src := &countingSource{contacts: oma(), gate: make(chan struct{})}
	clock := &fakeClock{now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	dir := NewDirectory(DirectoryConfig{Source: src, CountryCode: "49", Now: clock.Now, Logger: discard()})

	var wg sync.WaitGroup
	etags := make([]string, 20)
	for i := range etags {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, etag, err := dir.Phonebook(context.Background())
			if err != nil {
				t.Error(err)
			}
			etags[i] = etag
		}()
	}
	// Let all requests pile up behind the first fetch, then release it.
	deadline := time.Now().Add(2 * time.Second)
	for src.phonebookHits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(src.gate)
	wg.Wait()
	if n := src.phonebookHits.Load(); n != 1 {
		t.Fatalf("FRITZ!Box asked %d times for 20 concurrent requests, want 1", n)
	}
	for _, e := range etags {
		if e != etags[0] || e == "" {
			t.Fatalf("different ETags: %v", etags)
		}
	}

	clock.Advance(9 * time.Minute)
	_, etag, _ := dir.Phonebook(context.Background())
	if src.phonebookHits.Load() != 1 {
		t.Fatal("refetched within the 10 min cache")
	}
	clock.Advance(2 * time.Minute)
	_, etagAfter, _ := dir.Phonebook(context.Background())
	if src.phonebookHits.Load() != 2 {
		t.Fatal("not refetched after 10 min")
	}
	if etag != etagAfter {
		t.Fatal("ETag changed although the content did not (it must not cover updatedAt)")
	}
	src.mu.Lock()
	src.contacts = append(oma(), protocol.Contact{ID: "0-2", Name: "Opa", Numbers: []protocol.ContactNumber{{Number: "0301", Type: "home"}}})
	src.mu.Unlock()
	clock.Advance(11 * time.Minute)
	if _, changed, _ := dir.Phonebook(context.Background()); changed == etagAfter {
		t.Fatal("ETag unchanged although the content changed")
	}

	if _, err := dir.History(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	clock.Advance(29 * time.Second)
	_, _ = dir.History(context.Background(), 10)
	clock.Advance(2 * time.Second)
	_, _ = dir.History(context.Background(), 10)
	if n := src.historyHits.Load(); n != 2 {
		t.Fatalf("call list fetched %d times, want 2 (30 s cache)", n)
	}
}

func TestDirectoryFeaturesFollowLastFetch(t *testing.T) {
	src := &countingSource{contacts: oma()}
	clock := &fakeClock{now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	dir := NewDirectory(DirectoryConfig{Source: src, Now: clock.Now, Logger: discard()})
	if f := dir.Features(); len(f) != 0 {
		t.Fatalf("features before any fetch: %v", f)
	}
	dir.Warmup(context.Background())
	if f := dir.Features(); !reflect.DeepEqual(f, []string{protocol.FeatureFritzBoxPhonebook, protocol.FeatureFritzBoxHistory}) {
		t.Fatalf("features after warmup: %v", f)
	}
	src.mu.Lock()
	src.historyErr = &Error{Kind: KindUnsupported, Err: errNoCallList}
	src.mu.Unlock()
	clock.Advance(time.Minute)
	if _, err := dir.History(context.Background(), 10); err == nil {
		t.Fatal("expected error")
	}
	if f := dir.Features(); !reflect.DeepEqual(f, []string{protocol.FeatureFritzBoxPhonebook}) {
		t.Fatalf("features after failed call list: %v", f)
	}
}

func TestCallerNameLookup(t *testing.T) {
	src := &countingSource{contacts: []protocol.Contact{
		{ID: "0-1", Name: "Oma", Numbers: []protocol.ContactNumber{{Number: "030123456", Type: "home"}, {Number: "+491701234567", Type: "mobile"}}},
		{ID: "0-2", Name: "Praxis", Numbers: []protocol.ContactNumber{{Number: "0301111", Type: "work"}}},
		{ID: "0-3", Name: "Empfang", Numbers: []protocol.ContactNumber{{Number: "0301111", Type: "work"}}},
		{ID: "0-4", Name: "Oma", Numbers: []protocol.ContactNumber{{Number: "030123456", Type: "home"}}},
	}}
	dir := NewDirectory(DirectoryConfig{Source: src, CountryCode: "49", Logger: discard()})

	// No cache yet: no name, no waiting, but a background refresh starts.
	if name := dir.CallerName("030123456"); name != "" {
		t.Fatalf("name without cache: %q", name)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(dir.Features()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	for number, want := range map[string]string{
		"030123456":      "Oma", // same name twice is not ambiguous
		"+4930123456":    "Oma",
		"004930123456":   "Oma",
		"01701234567":    "Oma",
		"+49 170 123456": "",
		"0301111":        "", // two different names: ambiguous
		"":               "",
		"anonymous":      "",
	} {
		if got := dir.CallerName(number); got != want {
			t.Errorf("CallerName(%q) = %q, want %q", number, got, want)
		}
	}
}

func TestNumberNormalization(t *testing.T) {
	for in, want := range map[string]string{
		"030 / 12 34-56":    "030123456",
		"+49 (0)30 123456":  "+49030123456",
		"**620":             "**620",
		"*20#":              "*20#",
		"  +49 170 1234567": "+491701234567",
		"12+34":             "1234",
	} {
		if got := cleanNumber(in); got != want {
			t.Errorf("cleanNumber(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"+4930123456":      "030123456",
		"004930123456":     "030123456",
		"+49 (0)30 123456": "030123456",
		"030123456":        "030123456",
		"+4330123456":      "+4330123456",
	} {
		if got := nationalNumber(in, "49"); got != want {
			t.Errorf("nationalNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
