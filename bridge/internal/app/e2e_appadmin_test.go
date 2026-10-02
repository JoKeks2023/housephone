package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// Administration from the app (ADR-0009) through the running bridge: the
// first iPhone is admin, enrolls its Face ID key over the home network
// listener, renames another device and promotes it; everyone hears about
// it. Over the public listener nothing of this works.
func TestEndToEndAppAdmin(t *testing.T) {
	w := startWorld(t, func(c *config.Config) { c.Bridge.DataDir = shortDataDir(t) })
	adm := w.pairAndConnect(t)
	if adm.welcome.Admin == nil || !adm.welcome.Admin.Admin || adm.welcome.Admin.EnrollUntil == nil {
		t.Fatalf("first iPhone welcome.admin %+v", adm.welcome.Admin)
	}
	other := w.pairAndConnect(t)
	if other.welcome.Admin != nil {
		t.Fatalf("second iPhone is admin: %+v", other.welcome.Admin)
	}
	adm.expect(protocol.TypeDevicePaired, nil)

	lanBase := httpBase(t, w.lanURL)
	lan := *adm.client
	lan.BaseURL = lanBase
	adminKey, err := hp2.NewSoftwareKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, proof, err := hp2.AdminEnrollment(adminKey, lan.BridgeID, lan.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(protocol.AdminEnroll{AdminKey: pub, Proof: proof})
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()

	// The public listener refuses enrollment and every admin request.
	public := *adm.client
	public.AdminKey = adminKey
	if res, err := public.Do(ctx, http.MethodPost, "/v1/admin/enroll", body, nil); err != nil || res.Status != http.StatusForbidden {
		t.Fatalf("enroll over public: %v %+v", err, res)
	}

	res, err := lan.Do(ctx, http.MethodPost, "/v1/admin/enroll", body, nil)
	if err != nil || res.Status != http.StatusOK {
		t.Fatalf("enroll: %v %d %s", err, res.Status, res.Body)
	}
	for _, d := range []*device{adm, other} {
		var a protocol.AdminAction
		d.expect(protocol.TypeAdminAction, &a)
		if a.Action != protocol.AdminActionEnroll {
			t.Fatalf("announced %+v", a)
		}
	}

	lan.AdminKey = adminKey
	res, err = lan.Do(ctx, http.MethodGet, "/v1/admin/devices", nil, nil)
	var devs []admin.DeviceInfo
	if err != nil || res.Status != http.StatusOK || json.Unmarshal(res.Body, &devs) != nil || len(devs) != 2 {
		t.Fatalf("devices: %v %d %s", err, res.Status, res.Body)
	}
	if res, err := public.Do(ctx, http.MethodGet, "/v1/admin/devices", nil, nil); err != nil || res.Status != http.StatusForbidden {
		t.Fatalf("admin over public: %v %+v", err, res)
	}

	rename, _ := json.Marshal(protocol.AdminRename{Name: "Küche"})
	res, err = lan.Do(ctx, http.MethodPut, "/v1/admin/devices/"+other.client.DeviceID, rename, nil)
	if err != nil || res.Status != http.StatusOK {
		t.Fatalf("rename: %v %d %s", err, res.Status, res.Body)
	}
	var a protocol.AdminAction
	other.expect(protocol.TypeAdminAction, &a)
	if a.Action != protocol.AdminActionRename || a.Actor != "Test-iPhone" || a.Target != "Küche" {
		t.Fatalf("rename announced %+v", a)
	}
	adm.expect(protocol.TypeAdminAction, nil)

	// Promote the other iPhone from the app: it learns through admin.role.
	res, err = lan.Do(ctx, http.MethodPost, "/v1/admin/devices/"+other.client.DeviceID+"/promote", nil, nil)
	if err != nil || res.Status != http.StatusOK {
		t.Fatalf("promote: %v %d %s", err, res.Status, res.Body)
	}
	var role protocol.AdminRole
	other.expect(protocol.TypeAdminRole, &role)
	if !role.Admin || role.Enrolled || role.EnrollUntil == nil {
		t.Fatalf("admin.role %+v", role)
	}

	// Demoted through the admin socket, the first iPhone loses access at
	// once.
	client := dialAdmin(t, w.dataDir)
	if _, err := client.DemoteDevice(ctx, adm.client.DeviceID); err != nil {
		t.Fatal(err)
	}
	if res, err := lan.Do(ctx, http.MethodGet, "/v1/admin/status", nil, nil); err != nil || res.Status != http.StatusForbidden {
		t.Fatalf("after demotion: %v %+v", err, res)
	}
}
