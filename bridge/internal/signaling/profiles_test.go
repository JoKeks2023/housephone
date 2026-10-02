package signaling

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/profile"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// ADR-0008: two profiles, A (default, no own numbers) and B.
func withProfiles(c *Config) {
	c.Profiles = profile.NewSet(
		profile.Profile{ID: profile.DefaultID, Name: "Profil A"},
		profile.Profile{ID: "b", Name: "Profil B", Numbers: []string{"030 1234568"}, Phonebooks: []string{"1"}},
	)
}

func (ts *testServer) pairInto(t *testing.T, profileID, name string) *testDevice {
	t.Helper()
	pc, err := ts.pairing.CreateFor(name, profileID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ts.pairWith(t, pc.Code, protocol.PlatformIOS, name, "iPhone17,1")
}

func TestPairingIntoProfileAndWelcome(t *testing.T) {
	ts := newTestServer(t, withProfiles, func(c *Config) {
		c.Directory = &fakeDirectory{features: []string{protocol.FeatureFritzBoxPhonebook, protocol.FeatureFritzBoxHistory}}
	})
	b := ts.pairInto(t, "b", "iPhone B")
	dev, err := ts.devices.Get(b.ID)
	if err != nil || dev.Profile != "b" {
		t.Fatalf("stored device %+v %v", dev, err)
	}

	c, err := ts.dial(t, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.WS().CloseNow() })
	send(t, c, protocol.TypeHello, iPhoneHello)
	var welcome protocol.Welcome
	receive(t, c, protocol.TypeWelcome, &welcome)
	if welcome.Profile == nil || *welcome.Profile != (protocol.ProfileInfo{ID: "b", Name: "Profil B", Number: "030 1234568"}) || !welcome.SIPRegistered {
		t.Fatalf("welcome %+v profile %+v", welcome, welcome.Profile)
	}
	if !slices.Contains(welcome.Features, protocol.FeatureFritzBoxHistory) {
		t.Fatalf("profile with numbers lost the call list: %v", welcome.Features)
	}

	// Profile A has no own numbers: with two profiles it gets no call list.
	a := ts.pairDevice(t)
	ca := ts.connectHelloWelcome(t, a)
	if ca.Profile == nil || ca.Profile.ID != profile.DefaultID || slices.Contains(ca.Features, protocol.FeatureFritzBoxHistory) {
		t.Fatalf("welcome of profile A: %+v features %v", ca.Profile, ca.Features)
	}
}

func (ts *testServer) connectHelloWelcome(t *testing.T, d *testDevice) protocol.Welcome {
	t.Helper()
	c, err := ts.dial(t, d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.WS().CloseNow() })
	send(t, c, protocol.TypeHello, iPhoneHello)
	var welcome protocol.Welcome
	receive(t, c, protocol.TypeWelcome, &welcome)
	return welcome
}

func TestWatchJoinsItsIPhonesProfile(t *testing.T) {
	ts := newTestServer(t, withProfiles)
	phone := ts.pairInto(t, "b", "iPhone B")
	companion, err := ts.pairing.CreateCompanion(phone.ID, "Watch B", protocol.PlatformWatchOS, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	watch := ts.pairWith(t, companion.Code, protocol.PlatformWatchOS, "Watch", "Watch7,1")
	dev, err := ts.devices.Get(watch.ID)
	if err != nil || dev.ProfileID() != "b" {
		t.Fatalf("watch profile %q %v", dev.Profile, err)
	}
}

func TestDirectoryIsScopedToTheProfile(t *testing.T) {
	dir := &fakeDirectory{body: []byte(`{"contacts":[]}`), etag: `"e"`, history: []byte(`{"calls":[]}`)}
	ts := newTestServer(t, withProfiles, func(c *Config) { c.Directory = dir })
	a := ts.pairDevice(t)
	b := ts.pairInto(t, "b", "iPhone B")

	// B: its phonebooks and only the calls on its own numbers.
	if res := b.do(t, http.MethodGet, "/v1/phonebook", nil); res.Status != http.StatusOK {
		t.Fatalf("phonebook B: %d", res.Status)
	}
	if res := b.do(t, http.MethodGet, "/v1/history", nil); res.Status != http.StatusOK {
		t.Fatalf("history B: %d", res.Status)
	}
	dir.mu.Lock()
	books, own := dir.lastBooks, dir.lastOwn
	dir.mu.Unlock()
	if !slices.Equal(books, []string{"1"}) || !slices.Equal(own, []string{"030 1234568"}) {
		t.Fatalf("B asked for books %v, own %v", books, own)
	}

	// A has no own numbers: no call list at all (it can't be told apart).
	res := a.do(t, http.MethodGet, "/v1/history", nil)
	if res.Status != http.StatusServiceUnavailable || decodeError(t, res.Body).Code != protocol.ErrorFritzBoxUnavailable {
		t.Fatalf("history A: %d %s", res.Status, res.Body)
	}
	if res := a.do(t, http.MethodGet, "/v1/phonebook", nil); res.Status != http.StatusOK {
		t.Fatalf("phonebook A: %d", res.Status)
	}
	dir.mu.Lock()
	books = dir.lastBooks
	dir.mu.Unlock()
	if books != nil {
		t.Fatalf("A without phonebook choice asked for %v, want all", books)
	}

	// A device whose profile was removed from the configuration sees
	// nothing.
	if _, err := ts.devices.Update(a.ID, func(d *store.Device) { d.Profile = "gone" }); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/phonebook", "/v1/history"} {
		if res := a.do(t, http.MethodGet, path, nil); res.Status != http.StatusForbidden {
			t.Fatalf("%s for a removed profile: %d", path, res.Status)
		}
	}
}

func TestSingleProfileKeepsTheFullCallList(t *testing.T) {
	dir := &fakeDirectory{history: []byte(`{"calls":[]}`)}
	ts := newTestServer(t, func(c *Config) { c.Directory = dir })
	a := ts.pairDevice(t)
	if res := a.do(t, http.MethodGet, "/v1/history", nil); res.Status != http.StatusOK {
		t.Fatalf("history: %d", res.Status)
	}
	if dir.lastOwn != nil {
		t.Fatalf("a single profile filtered by %v", dir.lastOwn)
	}
}

func TestMovedDeviceIsReconnected(t *testing.T) {
	ts := newTestServer(t, withProfiles)
	phone := ts.pairDevice(t)
	c := ts.connectHello(t, phone, iPhoneHello)
	if _, err := ts.devices.Update(phone.ID, func(d *store.Device) { d.Profile = "b" }); err != nil {
		t.Fatal(err)
	}
	ts.srv.RevokeNow(phone.ID)
	if status := closeStatus(t, c); status != websocket.StatusCode(protocol.CloseProfileChanged) {
		t.Fatalf("close %v, want %d (profile changed)", status, protocol.CloseProfileChanged)
	}
	ts.hub.mu.Lock()
	revoked := slices.Clone(ts.hub.revoked)
	ts.hub.mu.Unlock()
	if !slices.Contains(revoked, phone.ID) {
		t.Fatalf("calls of the moved device not ended: %v", revoked)
	}
	// It reconnects into the new profile.
	if w := ts.connectHelloWelcome(t, phone); w.Profile == nil || w.Profile.ID != "b" {
		t.Fatalf("welcome after the move: %+v", w.Profile)
	}
}

func TestLanApprovalNeedsAKnownProfile(t *testing.T) {
	ts := newTestServer(t, withProfiles)
	if _, err := ts.srv.ApproveLanPairingFor("whatever", "nope"); err != ErrUnknownProfile {
		t.Fatalf("unknown profile: %v", err)
	}
}

func TestDevicePairedNoticeStaysInTheProfile(t *testing.T) {
	ts := newTestServer(t, withProfiles)
	a := ts.pairDevice(t)
	ca := ts.connectHello(t, a, iPhoneHello)
	b := ts.pairInto(t, "b", "iPhone B")
	cb := ts.connectHello(t, b, iPhoneHello)

	ts.pairInto(t, "b", "iPad B")
	var notice protocol.DevicePaired
	receive(t, cb, protocol.TypeDevicePaired, &notice)
	if notice.DeviceName != "iPad B" {
		t.Fatalf("notice %+v", notice)
	}
	expectNoMessage(t, ca)
}

// expectNoMessage fails if c receives a frame within 200 ms. A timeout
// closes the connection (coder/websocket), so it must be the last read.
func expectNoMessage(t *testing.T, c *hp2.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, plaintext, err := c.Read(ctx); err == nil {
		t.Fatalf("unexpected message %s", plaintext)
	}
}
