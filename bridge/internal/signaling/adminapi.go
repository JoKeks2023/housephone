package signaling

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Administration from the app (ADR-0009): /v1/admin/* on the private
// listener only. Every request is an HP2 request of the device (signed
// with the device key, answer sealed) and carries a second signature of
// the device's admin key in HP2-Admin. The admin key lives in the Secure
// Enclave and signs only after Face ID. The device must be an admin iPhone
// at the time of every request; a demotion takes effect at once.

// AdminEnrollWindow is how long a promoted device may enroll its admin key.
const AdminEnrollWindow = time.Hour

// AdminBackend runs admin actions for a device; the service it returns
// names that device as the actor of the announced actions.
type AdminBackend func(actor store.Device) admin.Service

// adminHandler serves an admin request; svc acts on behalf of dev.
type adminHandler func(w http.ResponseWriter, r *http.Request, dev store.Device, svc admin.Service)

// adminRoutes registers the admin endpoints. On the public listener every
// one answers 403 home_network_required.
func (s *Server) adminRoutes(mux *http.ServeMux, private bool) {
	routes := map[string]http.HandlerFunc{
		"POST /v1/admin/enroll":                    s.authed(s.httpAdminEnroll),
		"GET /v1/admin/status":                     s.adminAuthed(s.httpAdminStatus),
		"GET /v1/admin/stats":                      s.adminAuthed(s.httpAdminStats),
		"GET /v1/admin/profiles":                   s.adminAuthed(s.httpAdminProfiles),
		"GET /v1/admin/devices":                    s.adminAuthed(s.httpAdminDevices),
		"PUT /v1/admin/devices/{id}":               s.adminAuthed(s.httpAdminRename),
		"DELETE /v1/admin/devices/{id}":            s.adminAuthed(s.httpAdminRemove),
		"POST /v1/admin/devices/{id}/move":         s.adminAuthed(s.httpAdminMove),
		"POST /v1/admin/devices/{id}/promote":      s.adminAuthed(s.httpAdminPromote),
		"POST /v1/admin/devices/{id}/demote":       s.adminAuthed(s.httpAdminDemote),
		"GET /v1/admin/pairings/lan":               s.adminAuthed(s.httpAdminLanPairings),
		"POST /v1/admin/pairings/lan/{id}/approve": s.adminAuthed(s.httpAdminApprove),
		"POST /v1/admin/pairings/lan/{id}/deny":    s.adminAuthed(s.httpAdminDeny),
		"POST /v1/admin/pairings":                  s.adminAuthed(s.httpAdminInvite),
		"GET /v1/admin/pairings/{code}":            s.adminAuthed(s.httpAdminInviteState),
		"DELETE /v1/admin/pairings/{code}":         s.adminAuthed(s.httpAdminRevokeInvite),
	}
	for pattern, h := range routes {
		if private && s.cfg.Admin != nil {
			mux.HandleFunc(pattern, withDeadline(h))
		} else {
			mux.HandleFunc(pattern, s.adminHomeNetworkOnly)
		}
	}
}

// adminHomeNetworkOnly answers admin requests on the public listener (and
// when the bridge offers no admin API).
func (s *Server) adminHomeNetworkOnly(w http.ResponseWriter, r *http.Request) {
	s.writeSignedError(w, s.sessionFromHeader(r), http.StatusForbidden, protocol.Error{Code: protocol.ErrorHomeNetworkRequired, Message: "administration is only possible in the home network"})
}

// adminAuthed wraps an admin endpoint: HP2 authentication of the device,
// then the admin role and the HP2-Admin signature. Errors after the
// device check are sealed like every authenticated answer.
func (s *Server) adminAuthed(h adminHandler) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, dev store.Device) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		if !s.adminSignatureValid(r, dev, body) {
			s.warnClient("rejected admin request", s.clientIP(r), "device", dev.ID, "path", r.URL.Path)
			writeJSON(w, http.StatusForbidden, protocol.Error{Code: protocol.ErrorAdminRequired, Message: "admin role and Face ID signature required"})
			return
		}
		h(w, r, dev, s.cfg.Admin(dev))
	})
}

// adminSignatureValid checks that dev is an admin iPhone with an enrolled
// admin key that signed this request.
func (s *Server) adminSignatureValid(r *http.Request, dev store.Device, body []byte) bool {
	if !dev.Admin || !dev.CanBeAdmin() || dev.AdminKey == "" {
		return false
	}
	value := r.Header.Get(hp2.AdminHeader)
	if value == "" {
		return false
	}
	h, err := hp2.ParseAuthorization(r.Header.Get("Authorization"))
	if err != nil || h.DeviceID != dev.ID {
		return false
	}
	return hp2.VerifyAdminRequest(dev.AdminKey, value, h, r.Method, r.RequestURI, s.cfg.BridgeID, body)
}

// httpAdminEnroll handles POST /v1/admin/enroll: a promoted device stores
// its admin key within the enrollment window, signed by the device key
// (HP2) and with a proof that it holds the admin key. The window closes
// with the enrollment; a new key (after a Face ID change) needs a new
// promotion by an admin.
func (s *Server) httpAdminEnroll(w http.ResponseWriter, r *http.Request, dev store.Device) {
	var req protocol.AdminEnroll
	if err := decodeBody(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "invalid body"})
		return
	}
	now := s.cfg.Now()
	if !dev.AdminEnrollOpen(now) {
		writeJSON(w, http.StatusForbidden, protocol.Error{Code: protocol.ErrorAdminEnrollClosed, Message: "enrollment not open"})
		return
	}
	if req.AdminKey == dev.PublicKey || !hp2.VerifyAdminEnrollment(req.AdminKey, req.Proof, s.cfg.BridgeID, dev.ID) {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "invalid admin key or proof"})
		return
	}
	updated, err := s.cfg.Devices.Update(dev.ID, func(d *store.Device) {
		// Checked again under the store lock: a demotion in between wins.
		if d.AdminEnrollOpen(now) {
			d.AdminKey = req.AdminKey
			d.AdminEnrollUntil = time.Time{}
		}
	})
	if err != nil || updated.AdminKey != req.AdminKey {
		writeJSON(w, http.StatusForbidden, protocol.Error{Code: protocol.ErrorAdminEnrollClosed, Message: "enrollment not open"})
		return
	}
	s.log.Info("admin key enrolled", "device", dev.ID, "key", hp2.KeyFingerprint(req.AdminKey))
	s.AnnounceAdminAction(updated.Name, protocol.AdminActionEnroll, updated.Name, updated.ProfileID())
	writeJSON(w, http.StatusOK, AdminRoleOf(updated, now))
}

func (s *Server) httpAdminStatus(w http.ResponseWriter, _ *http.Request, _ store.Device, svc admin.Service) {
	writeJSON(w, http.StatusOK, svc.Status())
}

func (s *Server) httpAdminStats(w http.ResponseWriter, _ *http.Request, _ store.Device, svc admin.Service) {
	writeJSON(w, http.StatusOK, svc.Stats())
}

func (s *Server) httpAdminProfiles(w http.ResponseWriter, _ *http.Request, _ store.Device, svc admin.Service) {
	writeJSON(w, http.StatusOK, svc.Profiles())
}

func (s *Server) httpAdminDevices(w http.ResponseWriter, _ *http.Request, _ store.Device, svc admin.Service) {
	list, err := svc.Devices()
	s.adminReply(w, list, err)
}

func (s *Server) httpAdminRename(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	var req protocol.AdminRename
	if err := decodeBody(w, r, &req); err != nil || SanitizeName(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "name missing"})
		return
	}
	dev, err := svc.RenameDevice(r.PathValue("id"), req.Name)
	s.adminReply(w, dev, err)
}

func (s *Server) httpAdminRemove(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	res, err := svc.RemoveDevice(r.PathValue("id"), r.URL.Query().Get("keepCompanions") == "1")
	s.adminReply(w, res, err)
}

func (s *Server) httpAdminMove(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	var req protocol.AdminProfileChoice
	if err := decodeBody(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "invalid body"})
		return
	}
	res, err := svc.MoveDevice(r.PathValue("id"), req.Profile)
	s.adminReply(w, res, err)
}

func (s *Server) httpAdminPromote(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	dev, err := svc.PromoteDevice(r.PathValue("id"))
	s.adminReply(w, dev, err)
}

func (s *Server) httpAdminDemote(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	dev, err := svc.DemoteDevice(r.PathValue("id"))
	s.adminReply(w, dev, err)
}

func (s *Server) httpAdminLanPairings(w http.ResponseWriter, _ *http.Request, _ store.Device, svc admin.Service) {
	list := svc.LanPairings()
	if list == nil {
		list = []admin.LanPairingRequest{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) httpAdminApprove(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	var req protocol.AdminProfileChoice
	if err := decodeBody(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "invalid body"})
		return
	}
	dev, err := svc.ApproveLanPairing(r.PathValue("id"), req.Profile)
	s.adminReply(w, dev, err)
}

func (s *Server) httpAdminDeny(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	err := svc.DenyLanPairing(r.PathValue("id"))
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.adminReply(w, nil, err)
}

func (s *Server) httpAdminInvite(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	var req protocol.AdminInvite
	if err := decodeBody(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "invalid body"})
		return
	}
	info, err := svc.CreatePairing(req.Name, req.Profile)
	s.adminReply(w, info, err)
}

func (s *Server) httpAdminInviteState(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	st, err := svc.PairingState(r.PathValue("code"))
	s.adminReply(w, st, err)
}

func (s *Server) httpAdminRevokeInvite(w http.ResponseWriter, r *http.Request, _ store.Device, svc admin.Service) {
	if err := svc.RevokePairing(r.PathValue("code")); err != nil {
		s.adminReply(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminReply maps the admin service's errors to protocol errors.
func (s *Server) adminReply(w http.ResponseWriter, v any, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, v)
	case errors.Is(err, admin.ErrNotFound):
		writeJSON(w, http.StatusNotFound, protocol.Error{Code: protocol.ErrorNotFound, Message: "not found"})
	case errors.Is(err, admin.ErrUnknownProfile):
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "unknown profile"})
	case errors.Is(err, admin.ErrNotAllowed):
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorNotAllowed, Message: "not allowed"})
	default:
		s.log.Error("admin request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, protocol.Error{Code: protocol.ErrorInternal, Message: "internal error"})
	}
}

// AdminRoleOf is a device's admin state for welcome and admin.role.
func AdminRoleOf(dev store.Device, now time.Time) protocol.AdminRole {
	role := protocol.AdminRole{Admin: dev.Admin && dev.CanBeAdmin(), Enrolled: dev.AdminKey != ""}
	if !role.Admin {
		role.Enrolled = false
	}
	if dev.AdminEnrollOpen(now) {
		until := protocol.Timestamp(dev.AdminEnrollUntil)
		role.EnrollUntil = &until
	}
	return role
}

// adminRoleForWelcome is nil for devices that are not admin.
func adminRoleForWelcome(dev store.Device, now time.Time) *protocol.AdminRole {
	role := AdminRoleOf(dev, now)
	if !role.Admin {
		return nil
	}
	return &role
}

// SendAdminRole tells a connected device that its admin role changed.
func (s *Server) SendAdminRole(dev store.Device) {
	s.mu.Lock()
	sess := s.sessions[dev.ID]
	s.mu.Unlock()
	if sess != nil {
		sess.Send(protocol.MustEnvelope(protocol.TypeAdminRole, AdminRoleOf(dev, s.cfg.Now())))
	}
}

// AnnounceAdminAction tells every connected device about an admin action
// (ADR-0009: all devices see every admin action). The target's name only
// goes to devices of targetProfile and to admins; other profiles learn
// that something happened, not about whose device (ADR-0008).
func (s *Server) AnnounceAdminAction(actor, action, target, targetProfile string) {
	now := s.cfg.Now()
	full := protocol.AdminAction{Actor: actor, Action: action, Target: target, At: protocol.Timestamp(now)}
	anonymous := full
	anonymous.Target = ""
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
	s.log.Info("admin action", "actor", actor, "action", action)
	for _, sess := range sessions {
		msg := anonymous
		if targetProfile == "" || sess.profileID == targetProfile || s.isAdmin(sess.deviceID) {
			msg = full
		}
		sess.Send(protocol.MustEnvelope(protocol.TypeAdminAction, msg))
	}
}

func (s *Server) isAdmin(deviceID string) bool {
	dev, err := s.cfg.Devices.Get(deviceID)
	return err == nil && dev.Admin && dev.CanBeAdmin()
}
