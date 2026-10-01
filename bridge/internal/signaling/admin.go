package signaling

import "time"

// Online returns the connected devices and since when (admin API).
func (s *Server) Online() map[string]time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]time.Time, len(s.sessions))
	for id, sess := range s.sessions {
		out[id] = sess.connectedAt
	}
	return out
}

// SanitizeName cleans a device name like pairing does (admin rename).
func SanitizeName(s string) string { return sanitizeName(s) }

// RevokeNow closes the connection of a device that was just removed from
// the store or moved to another profile (admin API), without waiting for
// the periodic check.
func (s *Server) RevokeNow(deviceID string) {
	s.mu.Lock()
	sess := s.sessions[deviceID]
	s.mu.Unlock()
	if sess != nil {
		s.revokeIfRemoved(sess)
	}
}
