package signaling

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/coder/websocket"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// maxHTTPBody bounds request bodies of the HTTPS device endpoints.
const maxHTTPBody = 16 << 10

// The HTTPS device endpoints exist for the watch (v1.1): watchOS only allows
// WebSocket during a CallKit call, so pairing and push-token updates use
// plain HTTPS. Payloads are the same as the WebSocket messages.

// httpPair handles POST /v1/pair (body: pair payload, answer: pair.ok
// payload or error payload).
func (s *Server) httpPair(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	var p protocol.Pair
	decodeErr := decodeBody(w, r, &p)
	ok, pairErr := s.pair(p, decodeErr, ip)
	if pairErr != nil {
		writeJSON(w, pairErr.status, protocol.Error{Code: pairErr.code, Message: pairErr.message})
		return
	}
	writeJSON(w, http.StatusOK, ok)
}

// httpUpdateDevice handles PUT /v1/device (Bearer; body: device.update
// payload including mediaCapabilities and pushTopic).
func (s *Server) httpUpdateDevice(w http.ResponseWriter, r *http.Request) {
	dev, ok := s.httpAuthenticate(w, r)
	if !ok {
		return
	}
	var u protocol.DeviceUpdate
	if err := decodeBody(w, r, &u); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: err.Error()})
		return
	}
	changes := changesFromUpdate(u)
	if err := s.validateDeviceChanges(changes); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: err.Error()})
		return
	}
	s.applyDeviceChanges(dev.ID, changes)
	w.WriteHeader(http.StatusNoContent)
}

// httpDeleteDevice handles DELETE /v1/device (Bearer), like device.unpair.
func (s *Server) httpDeleteDevice(w http.ResponseWriter, r *http.Request) {
	dev, ok := s.httpAuthenticate(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Devices.Remove(dev.ID); err != nil && !errors.Is(err, store.ErrDeviceNotFound) {
		s.log.Error("unpairing device failed", "device", dev.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, protocol.Error{Code: protocol.ErrorInternal, Message: "Gerät konnte nicht entfernt werden"})
		return
	}
	s.mu.Lock()
	sess := s.sessions[dev.ID]
	s.mu.Unlock()
	if sess != nil {
		sess.close(websocket.StatusNormalClosure, "unpaired")
	}
	s.log.Info("device unpaired itself via HTTPS", "device", dev.ID)
	w.WriteHeader(http.StatusNoContent)
}

// httpCallStatus handles GET /v1/calls/{callId} (Bearer): the call from this
// device's point of view. A ringing watch polls it because watchOS does not
// let it open a WebSocket before the call is answered.
func (s *Server) httpCallStatus(w http.ResponseWriter, r *http.Request) {
	dev, ok := s.httpAuthenticate(w, r)
	if !ok {
		return
	}
	status, found := s.cfg.Hub.CallStatus(dev.ID, r.PathValue("callId"))
	if !found {
		writeJSON(w, http.StatusNotFound, protocol.Error{Code: protocol.ErrorCallNotFound, Message: "unknown call"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) httpAuthenticate(w http.ResponseWriter, r *http.Request) (store.Device, bool) {
	dev, ok := s.authenticate(r.Header.Get("Authorization"))
	if !ok {
		s.warnClient("rejected HTTPS authentication", s.clientIP(r), "path", r.URL.Path)
		writeJSON(w, http.StatusUnauthorized, protocol.Error{Code: protocol.ErrorUnauthorized, Message: "unauthorized"})
		return store.Device{}, false
	}
	return dev, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	body := http.MaxBytesReader(w, r.Body, maxHTTPBody)
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("request body is empty")
	}
	return json.Unmarshal(data, v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
