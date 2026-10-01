package signaling

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// fakeAdmin records which device acted. Methods the tests don't use panic
// through the nil embedded interface.
type fakeAdmin struct {
	admin.Service
	mu     *sync.Mutex
	actors *[]string
	actor  string
}

func (f fakeAdmin) record(what string) {
	f.mu.Lock()
	*f.actors = append(*f.actors, f.actor+":"+what)
	f.mu.Unlock()
}

func (f fakeAdmin) Status() admin.Status {
	f.record("status")
	return admin.Status{BridgeName: "Zuhause"}
}

func (f fakeAdmin) Devices() ([]admin.DeviceInfo, error) {
	f.record("devices")
	return []admin.DeviceInfo{{ID: "a", Profile: "default"}, {ID: "b", Profile: "b"}}, nil
}

func (f fakeAdmin) RenameDevice(id, name string) (admin.DeviceInfo, error) {
	f.record("rename " + id)
	if id != "known" {
		return admin.DeviceInfo{}, admin.ErrNotFound
	}
	return admin.DeviceInfo{ID: id, Name: name}, nil
}

func (f fakeAdmin) PromoteDevice(id string) (admin.DeviceInfo, error) {
	f.record("promote " + id)
	return admin.DeviceInfo{}, admin.ErrNotAllowed
}

type adminTest struct {
	*testServer
	mu     sync.Mutex
	actors []string
}

func newAdminTest(t *testing.T, opts ...func(*Config)) *adminTest {
	at := &adminTest{}
	opts = append(opts, func(c *Config) {
		c.LanURL = testLanURL
		c.Admin = func(actor store.Device) admin.Service {
			return fakeAdmin{mu: &at.mu, actors: &at.actors, actor: actor.Name}
		}
	})
	at.testServer = newTestServer(t, opts...)
	return at
}

func (at *adminTest) recorded() []string {
	at.mu.Lock()
	defer at.mu.Unlock()
	return append([]string(nil), at.actors...)
}

// enroll enrolls a fresh admin key for d and returns it.
func enroll(t *testing.T, d *testDevice) *hp2.SoftwareKey {
	t.Helper()
	key, err := hp2.NewSoftwareKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, proof, err := hp2.AdminEnrollment(key, testBridgeID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	res := d.do(t, http.MethodPost, "/v1/admin/enroll", protocol.AdminEnroll{AdminKey: pub, Proof: proof})
	if res.Status != http.StatusOK {
		t.Fatalf("enroll: %d %s", res.Status, res.Body)
	}
	var role protocol.AdminRole
	if err := json.Unmarshal(res.Body, &role); err != nil || !role.Admin || !role.Enrolled || role.EnrollUntil != nil {
		t.Fatalf("enroll answer %+v %v", role, err)
	}
	return key
}

// adminDo sends an admin request signed with adminKey (nil: none).
func adminDo(t *testing.T, d *testDevice, adminKey hp2.DeviceKey, method, path string, body any) hp2.Response {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	c := d.client()
	c.AdminKey = adminKey
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := c.Do(ctx, method, path, data, nil)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func wantCode(t *testing.T, res hp2.Response, status int, code string) {
	t.Helper()
	if res.Status != status || decodeError(t, res.Body).Code != code {
		t.Fatalf("want %d %s, got %d %s", status, code, res.Status, res.Body)
	}
}

// The first iPhone of a fresh bridge is admin and may enroll its Face ID
// key once; later devices and watches are not admin.
func TestFirstIPhoneIsAdminAndEnrollsOnce(t *testing.T) {
	at := newAdminTest(t)
	first := at.pairDevice(t)
	second := at.pairDevice(t)
	companion, err := at.pairing.CreateCompanion(first.ID, "Watch", protocol.PlatformWatchOS, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	watch := at.pairWith(t, companion.Code, protocol.PlatformWatchOS, "Watch", "Watch7,1")

	dev, _ := at.devices.Get(first.ID)
	if !dev.Admin || !dev.AdminEnrollOpen(time.Now()) || dev.AdminEnrollUntil.After(time.Now().Add(AdminEnrollWindow+time.Minute)) {
		t.Fatalf("first device %+v", dev)
	}
	for _, d := range []*testDevice{second, watch} {
		if dev, _ := at.devices.Get(d.ID); dev.Admin {
			t.Fatalf("%s is admin", d.ID)
		}
	}
	welcome := at.connectHelloWelcome(t, first)
	if welcome.Admin == nil || !welcome.Admin.Admin || welcome.Admin.Enrolled || welcome.Admin.EnrollUntil == nil {
		t.Fatalf("welcome.admin %+v", welcome.Admin)
	}
	if w := at.connectHelloWelcome(t, second); w.Admin != nil {
		t.Fatalf("welcome.admin for a non-admin %+v", w.Admin)
	}

	// A wrong proof (another key's signature) is refused.
	key, _ := hp2.NewSoftwareKey()
	other, _ := hp2.NewSoftwareKey()
	pub := hp2.B64(key.PublicKeyX963())
	_, wrongProof, _ := hp2.AdminEnrollment(other, testBridgeID, first.ID)
	wantCode(t, first.do(t, http.MethodPost, "/v1/admin/enroll", protocol.AdminEnroll{AdminKey: pub, Proof: wrongProof}), http.StatusBadRequest, protocol.ErrorBadRequest)
	// The device key cannot double as admin key.
	devPub, devProof, _ := hp2.AdminEnrollment(first.Key, testBridgeID, first.ID)
	wantCode(t, first.do(t, http.MethodPost, "/v1/admin/enroll", protocol.AdminEnroll{AdminKey: devPub, Proof: devProof}), http.StatusBadRequest, protocol.ErrorBadRequest)
	// Non-admins cannot enroll.
	sPub, sProof, _ := hp2.AdminEnrollment(key, testBridgeID, second.ID)
	wantCode(t, second.do(t, http.MethodPost, "/v1/admin/enroll", protocol.AdminEnroll{AdminKey: sPub, Proof: sProof}), http.StatusForbidden, protocol.ErrorAdminEnrollClosed)

	enroll(t, first)
	// The window closed with the enrollment: no second key without a new
	// promotion (a thief with the passcode cannot swap in their face).
	again, _ := hp2.NewSoftwareKey()
	aPub, aProof, _ := hp2.AdminEnrollment(again, testBridgeID, first.ID)
	wantCode(t, first.do(t, http.MethodPost, "/v1/admin/enroll", protocol.AdminEnroll{AdminKey: aPub, Proof: aProof}), http.StatusForbidden, protocol.ErrorAdminEnrollClosed)
}

// The enrollment window ends.
func TestEnrollWindowExpires(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	at := newAdminTest(t, func(c *Config) { c.Now = func() time.Time { return clock() } })
	d := at.pairDevice(t)
	now = now.Add(AdminEnrollWindow + time.Second)
	d.now = func() time.Time { return now }
	key, _ := hp2.NewSoftwareKey()
	pub, proof, _ := hp2.AdminEnrollment(key, testBridgeID, d.ID)
	wantCode(t, d.do(t, http.MethodPost, "/v1/admin/enroll", protocol.AdminEnroll{AdminKey: pub, Proof: proof}), http.StatusForbidden, protocol.ErrorAdminEnrollClosed)
}

// Every admin request needs an admin iPhone and the admin key's signature.
func TestAdminRequestsNeedTheAdminSignature(t *testing.T) {
	at := newAdminTest(t)
	adm := at.pairDevice(t)
	plain := at.pairDevice(t)
	key := enroll(t, adm)

	if res := adminDo(t, adm, key, http.MethodGet, "/v1/admin/status", nil); res.Status != http.StatusOK {
		t.Fatalf("admin status: %d %s", res.Status, res.Body)
	}
	if res := adminDo(t, adm, key, http.MethodPut, "/v1/admin/devices/known", protocol.AdminRename{Name: "Flur"}); res.Status != http.StatusOK {
		t.Fatalf("rename: %d %s", res.Status, res.Body)
	}
	wantCode(t, adminDo(t, adm, key, http.MethodPut, "/v1/admin/devices/nope", protocol.AdminRename{Name: "x"}), http.StatusNotFound, protocol.ErrorNotFound)
	wantCode(t, adminDo(t, adm, key, http.MethodPost, "/v1/admin/devices/watch/promote", nil), http.StatusBadRequest, protocol.ErrorNotAllowed)

	// Without HP2-Admin, with the device key in its place, or with another
	// key: refused, and the service is never called.
	before := len(at.recorded())
	wantCode(t, adminDo(t, adm, nil, http.MethodGet, "/v1/admin/status", nil), http.StatusForbidden, protocol.ErrorAdminRequired)
	wantCode(t, adminDo(t, adm, adm.Key, http.MethodGet, "/v1/admin/status", nil), http.StatusForbidden, protocol.ErrorAdminRequired)
	stranger, _ := hp2.NewSoftwareKey()
	wantCode(t, adminDo(t, adm, stranger, http.MethodGet, "/v1/admin/status", nil), http.StatusForbidden, protocol.ErrorAdminRequired)
	// A non-admin device, even with a key of its own, is refused.
	wantCode(t, adminDo(t, plain, stranger, http.MethodGet, "/v1/admin/devices", nil), http.StatusForbidden, protocol.ErrorAdminRequired)
	if got := at.recorded(); len(got) != before {
		t.Fatalf("service called for refused requests: %v", got[before:])
	}

	// The admin signature covers the request: a signature for another
	// path does not carry over.
	creq, _ := hp2.NewClientRequest(adm.Key, adm.ID, testBridgeID, http.MethodGet, "/v1/admin/devices", nil, time.Now())
	sig, _ := creq.AdminSignature(key, http.MethodGet, "/v1/admin/status", nil)
	res, raw := at.request(t, http.MethodGet, "/v1/admin/devices", http.Header{"Authorization": {creq.Authorization()}, hp2.AdminHeader: {sig}}, nil)
	keys, err := creq.VerifyAnswer(at.identity.PublicKey(), res.StatusCode, raw, res.Header.Get(hp2.BridgeHeader))
	if err != nil {
		t.Fatal(err)
	}
	opened, _ := hp2.OpenBody(keys.BridgeToDevice, raw)
	if res.StatusCode != http.StatusForbidden || decodeError(t, opened).Code != protocol.ErrorAdminRequired {
		t.Fatalf("signature for another path: %d %s", res.StatusCode, opened)
	}

	// Demotion takes effect with the next request.
	if _, err := at.devices.Update(adm.ID, func(d *store.Device) { d.Admin = false }); err != nil {
		t.Fatal(err)
	}
	wantCode(t, adminDo(t, adm, key, http.MethodGet, "/v1/admin/status", nil), http.StatusForbidden, protocol.ErrorAdminRequired)
}

// A captured admin request cannot be sent again.
func TestAdminRequestReplayRejected(t *testing.T) {
	at := newAdminTest(t)
	adm := at.pairDevice(t)
	key := enroll(t, adm)
	creq, _ := hp2.NewClientRequest(adm.Key, adm.ID, testBridgeID, http.MethodGet, "/v1/admin/status", nil, time.Now())
	sig, _ := creq.AdminSignature(key, http.MethodGet, "/v1/admin/status", nil)
	header := http.Header{"Authorization": {creq.Authorization()}, hp2.AdminHeader: {sig}}
	if res, _ := at.request(t, http.MethodGet, "/v1/admin/status", header, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("first: %d", res.StatusCode)
	}
	if res, _ := at.request(t, http.MethodGet, "/v1/admin/status", header, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay: %d", res.StatusCode)
	}
}

// A watch is never admin, even if the store says so.
func TestWatchIsNeverAdmin(t *testing.T) {
	at := newAdminTest(t)
	phone := at.pairDevice(t)
	companion, _ := at.pairing.CreateCompanion(phone.ID, "Watch", protocol.PlatformWatchOS, time.Now())
	watch := at.pairWith(t, companion.Code, protocol.PlatformWatchOS, "Watch", "Watch7,1")
	key, _ := hp2.NewSoftwareKey()
	if _, err := at.devices.Update(watch.ID, func(d *store.Device) {
		d.Admin, d.AdminKey, d.AdminEnrollUntil = true, hp2.B64(key.PublicKeyX963()), time.Now().Add(time.Hour)
	}); err != nil {
		t.Fatal(err)
	}
	wantCode(t, adminDo(t, watch, key, http.MethodGet, "/v1/admin/status", nil), http.StatusForbidden, protocol.ErrorAdminRequired)
	other, _ := hp2.NewSoftwareKey()
	pub, proof, _ := hp2.AdminEnrollment(other, testBridgeID, watch.ID)
	wantCode(t, watch.do(t, http.MethodPost, "/v1/admin/enroll", protocol.AdminEnroll{AdminKey: pub, Proof: proof}), http.StatusForbidden, protocol.ErrorAdminEnrollClosed)
}

// Administration exists only on the private listener: the public one
// answers home_network_required, and the private one refuses loopback
// (cloudflared connects from there).
func TestAdminOnlyInTheHomeNetwork(t *testing.T) {
	at := newAdminTest(t)
	adm := at.pairDevice(t)
	key := enroll(t, adm)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	public := startListener(t, at.srv.PublicHandler())
	c := publicClient(adm, public.URL)
	c.AdminKey = key
	for _, path := range []string{"/v1/admin/status", "/v1/admin/devices"} {
		res, err := c.Do(ctx, http.MethodGet, path, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantCode(t, res, http.StatusForbidden, protocol.ErrorHomeNetworkRequired)
	}
	other, _ := hp2.NewSoftwareKey()
	pub, proof, _ := hp2.AdminEnrollment(other, testBridgeID, adm.ID)
	body, _ := json.Marshal(protocol.AdminEnroll{AdminKey: pub, Proof: proof})
	res, err := c.Do(ctx, http.MethodPost, "/v1/admin/enroll", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCode(t, res, http.StatusForbidden, protocol.ErrorHomeNetworkRequired)

	loopback := startListener(t, at.srv.PrivateHandler(mustNets(t, config.Bridge{Tailscale: true})))
	c = publicClient(adm, loopback.URL)
	c.AdminKey = key
	res, err = c.Do(ctx, http.MethodGet, "/v1/admin/status", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantCode(t, res, http.StatusForbidden, protocol.ErrorHomeNetworkRequired)
	if got := at.recorded(); len(got) != 0 {
		t.Fatalf("service called: %v", got)
	}
}

// Without an admin backend there is no admin API.
func TestNoAdminBackend(t *testing.T) {
	ts := newTestServer(t)
	d := ts.pairDevice(t)
	wantCode(t, d.do(t, http.MethodGet, "/v1/admin/status", nil), http.StatusForbidden, protocol.ErrorHomeNetworkRequired)
}

// Every device hears about admin actions; the target's name only reaches
// its own profile and admins (ADR-0008).
func TestAdminActionsAreAnnounced(t *testing.T) {
	at := newAdminTest(t, withProfiles)
	adm := at.pairDevice(t)
	enroll(t, adm)
	sameProfile := at.pairDevice(t)
	otherProfile := at.pairInto(t, "b", "iPhone B")
	conns := map[string]*hp2.Conn{}
	for name, d := range map[string]*testDevice{"admin": adm, "same": sameProfile, "other": otherProfile} {
		conns[name] = at.connectHello(t, d, protocol.Hello{AppVersion: "t", Platform: protocol.PlatformIOS})
	}
	at.srv.AnnounceAdminAction("iPhone", protocol.AdminActionRemove, "Altes iPad", "default")
	for name, c := range conns {
		var a protocol.AdminAction
		receive(t, c, protocol.TypeAdminAction, &a)
		wantTarget := "Altes iPad"
		if name == "other" {
			wantTarget = ""
		}
		if a.Actor != "iPhone" || a.Action != protocol.AdminActionRemove || a.Target != wantTarget {
			t.Fatalf("%s got %+v", name, a)
		}
	}

	// A promotion reaches the promoted device as admin.role.
	dev, _ := at.devices.Update(sameProfile.ID, func(d *store.Device) { d.Admin, d.AdminEnrollUntil = true, time.Now().Add(time.Hour) })
	at.srv.SendAdminRole(dev)
	var role protocol.AdminRole
	receive(t, conns["same"], protocol.TypeAdminRole, &role)
	if !role.Admin || role.Enrolled || role.EnrollUntil == nil {
		t.Fatalf("admin.role %+v", role)
	}
}
