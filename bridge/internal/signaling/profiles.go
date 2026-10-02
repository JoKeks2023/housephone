package signaling

import (
	"github.com/JoKeks2023/housephone/bridge/internal/profile"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Household profiles (ADR-0008): a device only gets its own profile's
// line, call list and phonebooks. A device whose profile is no longer in
// the configuration gets nothing until the admin moves it.

// profileOf returns the device's profile; ok is false if the profile was
// removed from the configuration.
func (s *Server) profileOf(dev store.Device) (profile.Profile, bool) {
	return s.cfg.Profiles.Get(dev.ProfileID())
}

// profileInfo is the profile part of welcome.
func (s *Server) profileInfo(dev store.Device) *protocol.ProfileInfo {
	p, ok := s.profileOf(dev)
	if !ok {
		return &protocol.ProfileInfo{ID: dev.ProfileID(), Name: "?"}
	}
	return &protocol.ProfileInfo{ID: p.ID, Name: p.Name, Number: p.Number()}
}

// featuresFor lists the v1.2 features the device may use: without own
// numbers a profile of a multi-profile household sees no call list, and a
// device of a removed profile sees nothing.
func (s *Server) featuresFor(dev store.Device) []string {
	p, ok := s.profileOf(dev)
	if !ok {
		return nil
	}
	var out []string
	for _, f := range s.features() {
		if f == protocol.FeatureFritzBoxHistory && !s.cfg.Profiles.HistoryAllowed(p) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ProfileSet is the configured profiles.
func (s *Server) ProfileSet() profile.Set { return s.cfg.Profiles }

// ValidProfile reports whether id names a configured profile.
func (s *Server) ValidProfile(id string) bool {
	_, ok := s.cfg.Profiles.Get(id)
	return ok
}
