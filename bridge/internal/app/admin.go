package app

import (
	"context"
	"errors"
	"slices"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/signaling"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
	"github.com/JoKeks2023/housephone/bridge/internal/version"
)

// adminService implements admin.Service for the running bridge.
type adminService struct{ b *Bridge }

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
		SIPRegistered:      b.sip.Registered(),
		Registrar:          b.registrarIP,
		SIPUser:            b.cfg.SIP.Username,
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
		out = append(out, deviceInfo(d, names, online))
	}
	return out, nil
}

func deviceInfo(d store.Device, names map[string]string, online map[string]time.Time) admin.DeviceInfo {
	info := admin.DeviceInfo{
		ID: d.ID, Name: d.Name, Platform: d.Platform, Model: d.Model,
		KeyFingerprint: hp2.KeyFingerprint(d.PublicKey),
		Media:          protocol.MediaWebRTC,
		PairedBy:       d.PairedBy, PairedByName: names[d.PairedBy],
		CreatedAt: d.CreatedAt, LastSeen: d.LastSeen,
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
	return deviceInfo(dev, map[string]string{}, s.b.signaling.Online()), nil
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
		res.Removed = append(res.Removed, deviceInfo(d, nil, online))
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
			res.Kept = append(res.Kept, deviceInfo(c, nil, online))
			continue
		}
		if err := remove(c); err != nil {
			return res, err
		}
	}
	s.b.log.Info("devices removed via admin", "count", len(res.Removed))
	return res, nil
}

func (s adminService) CreatePairing(name string) (admin.PairingInfo, error) {
	pc, err := s.b.Pairing.Create(signaling.SanitizeName(name), time.Now())
	if err != nil {
		return admin.PairingInfo{}, err
	}
	fp := s.b.Key.Fingerprint()
	return admin.PairingInfo{
		Code: pc.Code, Grouped: hp2.GroupCode(pc.Code), Fingerprint: fp, ExpiresAt: pc.ExpiresAt,
		Link: PairingLink(s.b.cfg.Bridge.PublicURL, s.b.lanURL, pc.Code, fp, s.b.cfg.Bridge.Name),
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
			info := deviceInfo(dev, nil, s.b.signaling.Online())
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
	_, err := s.b.Pairing.Revoke(code)
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
	out := map[string]any{}
	if data, err := yaml.Marshal(c); err == nil {
		_ = yaml.Unmarshal(data, &out)
	}
	return out
}
