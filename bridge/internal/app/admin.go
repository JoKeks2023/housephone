package app

import (
	"context"
	"errors"
	"slices"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/profile"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/signaling"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
	"github.com/JoKeks2023/housephone/bridge/internal/version"
)

// adminService implements admin.Service for the running bridge. actor is
// the admin device acting through the app (ADR-0009); nil for the server
// itself (CLI, TUI, dashboard).
type adminService struct {
	b     *Bridge
	actor *store.Device
}

// actorName names who acted in admin.action: the admin device, or "" for
// the server.
func (s adminService) actorName() string {
	if s.actor == nil {
		return ""
	}
	return s.actor.Name
}

// announce tells all devices about an admin action (ADR-0009).
func (s adminService) announce(action, target, targetProfile string) {
	s.b.signaling.AnnounceAdminAction(s.actorName(), action, target, targetProfile)
}

var _ admin.Service = adminService{}

func (s adminService) Status() admin.Status {
	b := s.b
	st := admin.Status{
		Version:            version.Version,
		BridgeName:         b.cfg.Bridge.Name,
		BridgeID:           b.Identity.ID,
		Fingerprint:        b.Key.Fingerprint(),
		StartedAt:          b.startedAt,
		PublicURL:          b.cfg.Bridge.PublicURL,
		Listen:             b.Addr(),
		PrivateListen:      b.PrivateAddr(),
		LanURL:             b.lanURL,
		SIPRegistered:      b.manager.SIPRegistered(),
		Registrar:          b.registrarIP,
		SIPUser:            b.cfg.SIP.Username,
		Profiles:           s.Profiles(),
		PublicIP:           b.publicIP.Get(),
		PublicIPSource:     b.publicIP.Source(),
		MediaPort:          b.cfg.Media.UDPPort,
		APNsConfigured:     b.cfg.APNs.Enabled(),
		APNsTopic:          b.cfg.APNs.Topic,
		LastPush:           b.recorder.Stats().LastPush,
		FritzBoxConfigured: b.directory != nil,
		ActiveCalls:        b.manager.ActiveCalls(),
	}
	if b.directory != nil {
		st.FritzBoxFeatures = b.directory.Features()
	}
	if list, err := b.Devices.List(); err == nil {
		st.DevicesTotal = len(list)
	}
	st.DevicesOnline = len(b.signaling.Online())
	return st
}

// Profiles lists the household profiles with their line and devices
// (ADR-0008).
func (s adminService) Profiles() []admin.ProfileInfo {
	b := s.b
	devices, _ := b.Devices.List()
	online := b.signaling.Online()
	var out []admin.ProfileInfo
	for _, p := range b.profiles.List() {
		info := admin.ProfileInfo{
			ID: p.ID, Name: p.Name, SIPUser: p.SIPUser, Numbers: p.Numbers, Phonebooks: p.Phonebooks,
			Registered:     b.manager.SIPRegisteredFor(p.ID),
			HistoryAllowed: b.profiles.HistoryAllowed(p),
		}
		for _, d := range devices {
			if d.ProfileID() == p.ID {
				info.Devices++
				if _, ok := online[d.ID]; ok {
					info.DevicesOnline++
				}
			}
		}
		out = append(out, info)
	}
	return out
}

func (s adminService) Devices() ([]admin.DeviceInfo, error) {
	list, err := s.b.Devices.List()
	if err != nil {
		return nil, err
	}
	online := s.b.signaling.Online()
	names := make(map[string]string, len(list))
	for _, d := range list {
		names[d.ID] = d.Name
	}
	out := make([]admin.DeviceInfo, 0, len(list))
	for _, d := range list {
		out = append(out, s.deviceInfo(d, names, online))
	}
	return out, nil
}

func (s adminService) deviceInfo(d store.Device, names map[string]string, online map[string]time.Time) admin.DeviceInfo {
	info := admin.DeviceInfo{
		ID: d.ID, Name: d.Name, Platform: d.Platform, Model: d.Model,
		KeyFingerprint: hp2.KeyFingerprint(d.PublicKey),
		Media:          protocol.MediaWebRTC,
		PairedBy:       d.PairedBy, PairedByName: names[d.PairedBy],
		Profile:   d.ProfileID(),
		CreatedAt: d.CreatedAt, LastSeen: d.LastSeen,
		Admin: d.Admin && d.CanBeAdmin(), AdminEnrolled: d.Admin && d.AdminKey != "",
	}
	if d.AdminEnrollOpen(time.Now()) {
		info.AdminEnrollUntil = d.AdminEnrollUntil
	}
	if p, ok := s.b.profiles.Get(d.ProfileID()); ok {
		info.ProfileName = p.Name
	} else {
		info.ProfileName = "(entfernt)"
	}
	if slices.Contains(d.MediaCapabilities, protocol.MediaWebSocketPCMA) && !slices.Contains(d.MediaCapabilities, protocol.MediaWebRTC) {
		info.Media = protocol.MediaWebSocketPCMA
	}
	if d.PushToken != "" {
		info.Push = d.PushEnvironment
		if info.Push == "" {
			info.Push = "ja"
		}
	}
	if since, ok := online[d.ID]; ok {
		info.Online, info.OnlineSince = true, since
	}
	return info
}

func (s adminService) RenameDevice(id, name string) (admin.DeviceInfo, error) {
	name = signaling.SanitizeName(name)
	if name == "" {
		return admin.DeviceInfo{}, errors.New("leerer Name")
	}
	dev, err := s.b.Devices.Update(id, func(d *store.Device) { d.Name = name })
	if errors.Is(err, store.ErrDeviceNotFound) {
		return admin.DeviceInfo{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.DeviceInfo{}, err
	}
	s.announce(protocol.AdminActionRename, dev.Name, dev.ProfileID())
	return s.deviceInfo(dev, map[string]string{}, s.b.signaling.Online()), nil
}

// RemoveDevice removes a device (and, unless keepCompanions, the watches
// paired through it) and cuts off their connections right away.
func (s adminService) RemoveDevice(id string, keepCompanions bool) (admin.RemoveResult, error) {
	reg := s.b.Devices
	dev, err := reg.Get(id)
	if errors.Is(err, store.ErrDeviceNotFound) {
		return admin.RemoveResult{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.RemoveResult{}, err
	}
	companions, err := reg.Companions(id)
	if err != nil {
		return admin.RemoveResult{}, err
	}
	var res admin.RemoveResult
	online := s.b.signaling.Online()
	remove := func(d store.Device) error {
		if err := reg.Remove(d.ID); err != nil && !errors.Is(err, store.ErrDeviceNotFound) {
			return err
		}
		s.b.signaling.RevokeNow(d.ID)
		res.Removed = append(res.Removed, s.deviceInfo(d, nil, online))
		return nil
	}
	if err := remove(dev); err != nil {
		return res, err
	}
	if err := s.b.Pairing.RemoveByParent(id); err != nil {
		return res, err
	}
	for _, c := range companions {
		if keepCompanions {
			res.Kept = append(res.Kept, s.deviceInfo(c, nil, online))
			continue
		}
		if err := remove(c); err != nil {
			return res, err
		}
	}
	s.b.log.Info("devices removed via admin", "count", len(res.Removed))
	s.announce(protocol.AdminActionRemove, dev.Name, dev.ProfileID())
	return res, nil
}

// MoveDevice moves a device and the watches paired through it to another
// profile; their connections are closed so they reconnect into it.
func (s adminService) MoveDevice(id, profileID string) (admin.MoveResult, error) {
	p, ok := s.b.profiles.Get(profileID)
	if !ok {
		return admin.MoveResult{}, admin.ErrUnknownProfile
	}
	stored := p.ID
	if stored == profile.DefaultID {
		stored = ""
	}
	reg := s.b.Devices
	dev, err := reg.Get(id)
	if errors.Is(err, store.ErrDeviceNotFound) {
		return admin.MoveResult{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.MoveResult{}, err
	}
	companions, err := reg.Companions(id)
	if err != nil {
		return admin.MoveResult{}, err
	}
	var res admin.MoveResult
	online := s.b.signaling.Online()
	for _, d := range append([]store.Device{dev}, companions...) {
		moved, err := reg.Update(d.ID, func(d *store.Device) { d.Profile = stored })
		if errors.Is(err, store.ErrDeviceNotFound) {
			continue
		}
		if err != nil {
			return res, err
		}
		s.b.signaling.RevokeNow(d.ID)
		res.Moved = append(res.Moved, s.deviceInfo(moved, nil, online))
	}
	s.b.log.Info("devices moved via admin", "count", len(res.Moved), "profile", p.ID)
	// Both profiles may see the name: the old one loses the device, the
	// new one gains it.
	s.announce(protocol.AdminActionMove, dev.Name, dev.ProfileID())
	return res, nil
}

// PromoteDevice makes an iPhone admin and opens the window in which it
// enrolls its Face ID key (ADR-0009). Promoting an admin again reopens the
// window, e.g. after Face ID was set up anew; its old key stays valid
// until the new one is enrolled.
func (s adminService) PromoteDevice(id string) (admin.DeviceInfo, error) {
	dev, err := s.b.Devices.Get(id)
	if errors.Is(err, store.ErrDeviceNotFound) {
		return admin.DeviceInfo{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.DeviceInfo{}, err
	}
	if !dev.CanBeAdmin() {
		return admin.DeviceInfo{}, admin.ErrNotAllowed
	}
	until := time.Now().Add(signaling.AdminEnrollWindow).UTC()
	dev, err = s.b.Devices.Update(id, func(d *store.Device) {
		d.Admin = true
		d.AdminEnrollUntil = until
	})
	if err != nil {
		return admin.DeviceInfo{}, err
	}
	s.b.log.Info("device promoted to admin", "device", dev.ID)
	s.b.signaling.SendAdminRole(dev)
	s.announce(protocol.AdminActionPromote, dev.Name, "")
	return s.deviceInfo(dev, nil, s.b.signaling.Online()), nil
}

// DemoteDevice takes the admin role and key away; it takes effect with
// the next request.
func (s adminService) DemoteDevice(id string) (admin.DeviceInfo, error) {
	dev, err := s.b.Devices.Update(id, func(d *store.Device) {
		d.Admin = false
		d.AdminKey = ""
		d.AdminEnrollUntil = time.Time{}
	})
	if errors.Is(err, store.ErrDeviceNotFound) {
		return admin.DeviceInfo{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.DeviceInfo{}, err
	}
	s.b.log.Info("device demoted", "device", dev.ID)
	s.b.signaling.SendAdminRole(dev)
	s.announce(protocol.AdminActionDemote, dev.Name, "")
	return s.deviceInfo(dev, nil, s.b.signaling.Online()), nil
}

func (s adminService) CreatePairing(name, profileID string) (admin.PairingInfo, error) {
	p, ok := s.b.profiles.Get(profileID)
	if !ok {
		return admin.PairingInfo{}, admin.ErrUnknownProfile
	}
	stored := p.ID
	if stored == profile.DefaultID {
		stored = ""
	}
	pc, err := s.b.Pairing.CreateFor(signaling.SanitizeName(name), stored, time.Now())
	if err != nil {
		return admin.PairingInfo{}, err
	}
	fp := s.b.Key.Fingerprint()
	s.announce(protocol.AdminActionInvite, pc.Name, p.ID)
	return admin.PairingInfo{
		Code: pc.Code, Grouped: hp2.GroupCode(pc.Code), Fingerprint: fp, ExpiresAt: pc.ExpiresAt,
		Link:    PairingLink(s.b.cfg.Bridge.PublicURL, s.b.lanURL, pc.Code, fp, s.b.cfg.Bridge.Name),
		Profile: p.ID, ProfileName: p.Name,
	}, nil
}

func (s adminService) PairingState(code string) (admin.PairingState, error) {
	now := time.Now()
	used, found, err := s.b.Pairing.UsedBy(code, now)
	if err != nil {
		return admin.PairingState{}, err
	}
	if found {
		st := admin.PairingState{Used: true}
		if dev, err := s.b.Devices.Get(used.DeviceID); err == nil {
			info := s.deviceInfo(dev, nil, s.b.signaling.Online())
			st.Device = &info
		}
		return st, nil
	}
	pending, err := s.b.Pairing.Pending(now)
	if err != nil {
		return admin.PairingState{}, err
	}
	for _, pc := range pending {
		if pc.Code == code {
			return admin.PairingState{}, nil
		}
	}
	return admin.PairingState{Expired: true}, nil
}

func (s adminService) RevokePairing(code string) error {
	revoked, err := s.b.Pairing.Revoke(code)
	if err == nil && revoked {
		s.announce(protocol.AdminActionRevokeInvite, "", "")
	}
	return err
}

func (s adminService) LanPairings() []admin.LanPairingRequest {
	var out []admin.LanPairingRequest
	for _, p := range s.b.signaling.LanPairings() {
		out = append(out, admin.LanPairingRequest{
			ID: p.ID, DeviceName: p.DeviceName, Platform: p.Platform, Model: p.Model, IP: p.IP,
			SAS: p.SAS, KeyFingerprint: p.KeyFingerprint, CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt,
		})
	}
	return out
}

func (s adminService) ApproveLanPairing(id, profileID string) (admin.DeviceInfo, error) {
	dev, err := s.b.signaling.ApproveLanPairingFor(id, profileID)
	if errors.Is(err, signaling.ErrLanPairingNotFound) {
		return admin.DeviceInfo{}, admin.ErrNotFound
	}
	if errors.Is(err, signaling.ErrUnknownProfile) {
		return admin.DeviceInfo{}, admin.ErrUnknownProfile
	}
	if err != nil {
		return admin.DeviceInfo{}, err
	}
	s.announce(protocol.AdminActionApprove, dev.Name, dev.ProfileID())
	return s.deviceInfo(dev, nil, s.b.signaling.Online()), nil
}

func (s adminService) DenyLanPairing(id string) error {
	err := s.b.signaling.DenyLanPairing(id)
	if err == nil {
		s.announce(protocol.AdminActionDeny, "", "")
	}
	if errors.Is(err, signaling.ErrLanPairingNotFound) {
		return admin.ErrNotFound
	}
	return err
}

func (s adminService) Calls() admin.CallsView {
	active, recent := s.b.recorder.Calls()
	return admin.CallsView{Active: active, Recent: recent}
}

func (s adminService) Stats() admin.Stats { return s.b.recorder.Stats() }

func (s adminService) Logs(after uint64) admin.LogsView {
	if s.b.logRing == nil {
		return admin.LogsView{}
	}
	lines, next := s.b.logRing.Since(after)
	return admin.LogsView{Lines: lines, Next: next}
}

func (s adminService) SelfTest(context.Context) []admin.Check {
	b := s.b
	in := admin.CheckInput{
		Registrar: b.cfg.SIP.Registrar, RegistrarIP: b.registrarIP,
		SIPRegistered: b.sip.Registered(), SIPUser: b.cfg.SIP.Username,
		Profiles:           s.Profiles(),
		FritzBoxConfigured: b.directory != nil,
		APNsConfigured:     b.cfg.APNs.Enabled(), APNsKeyLoaded: b.apnsLoaded,
		LastPush: b.recorder.Stats().LastPush,
		PublicIP: b.publicIP.Get(), PublicIPSource: b.publicIP.Source(),
		PublicURL: b.cfg.Bridge.PublicURL, Listen: b.Addr(), MediaPort: b.cfg.Media.UDPPort,
	}
	if b.directory != nil {
		in.FritzBoxFeatures = b.directory.Features()
	}
	if list, err := b.Devices.List(); err == nil {
		in.DevicesTotal = len(list)
		for _, d := range list {
			if d.PushToken != "" {
				in.DevicesWithPush++
			}
		}
	}
	return admin.RunChecks(in)
}

// Config returns the effective configuration with secrets masked.
func (s adminService) Config() map[string]any {
	c := s.b.cfg
	if c.SIP.Password != "" {
		c.SIP.Password = "••••••"
	}
	if c.FritzBox.Password != "" {
		c.FritzBox.Password = "••••••"
	}
	c.Lines = append([]config.Line(nil), c.Lines...)
	for i := range c.Lines {
		if c.Lines[i].SIP.Password != "" {
			c.Lines[i].SIP.Password = "••••••"
		}
	}
	out := map[string]any{}
	if data, err := yaml.Marshal(c); err == nil {
		_ = yaml.Unmarshal(data, &out)
	}
	return out
}
