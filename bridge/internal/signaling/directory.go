package signaling

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// The v1.2 endpoints serve the FRITZ!Box phonebook and call list to the
// iPhone and the watch (ADR-0003).

const (
	historyDefaultLimit = 100
	historyMaxLimit     = 500
)

const notConfiguredMessage = "Telefonbuch und Anrufliste sind in der Bridge nicht eingerichtet (fritzbox.username in config.yaml)."

const noProfileMessage = "Das Profil dieses Geräts gibt es auf der Bridge nicht mehr."

const noNumbersMessage = "Für dein Profil sind keine eigenen Nummern eingetragen, deshalb zeigt die Bridge keine Anrufliste."

// httpPhonebook handles GET /v1/phonebook (HP2, ETag / If-None-Match).
func (s *Server) httpPhonebook(w http.ResponseWriter, r *http.Request, dev store.Device) {
	if s.cfg.Directory == nil {
		writeJSON(w, http.StatusServiceUnavailable, protocol.Error{Code: protocol.ErrorFritzBoxUnavailable, Message: notConfiguredMessage})
		return
	}
	p, ok := s.profileOf(dev)
	if !ok {
		writeJSON(w, http.StatusForbidden, protocol.Error{Code: protocol.ErrorUnauthorized, Message: noProfileMessage})
		return
	}
	body, etag, err := s.cfg.Directory.Phonebook(r.Context(), p.Phonebooks)
	if err != nil {
		s.writeFritzBoxError(w, err)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// httpHistory handles GET /v1/history?limit=n (HP2; 1-500, default 100).
func (s *Server) httpHistory(w http.ResponseWriter, r *http.Request, dev store.Device) {
	limit := historyDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > historyMaxLimit {
			writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "limit must be between 1 and 500"})
			return
		}
		limit = n
	}
	if s.cfg.Directory == nil {
		writeJSON(w, http.StatusServiceUnavailable, protocol.Error{Code: protocol.ErrorFritzBoxUnavailable, Message: notConfiguredMessage})
		return
	}
	// ADR-0008: with several profiles each one only sees the calls on its
	// own numbers, and a profile without numbers sees none.
	p, ok := s.profileOf(dev)
	if !ok {
		writeJSON(w, http.StatusForbidden, protocol.Error{Code: protocol.ErrorUnauthorized, Message: noProfileMessage})
		return
	}
	if !s.cfg.Profiles.HistoryAllowed(p) {
		writeJSON(w, http.StatusServiceUnavailable, protocol.Error{Code: protocol.ErrorFritzBoxUnavailable, Message: noNumbersMessage})
		return
	}
	var own []string
	if s.cfg.Profiles.Multi() {
		own = p.Numbers
	}
	body, err := s.cfg.Directory.History(r.Context(), limit, own)
	if err != nil {
		s.writeFritzBoxError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) writeFritzBoxError(w http.ResponseWriter, err error) {
	message := "Die FRITZ!Box ist nicht erreichbar."
	var um interface{ UserMessage() string }
	if errors.As(err, &um) {
		message = um.UserMessage()
	}
	writeJSON(w, http.StatusServiceUnavailable, protocol.Error{Code: protocol.ErrorFritzBoxUnavailable, Message: message})
}

// features lists the v1.2 features for welcome.
func (s *Server) features() []string {
	if s.cfg.Directory == nil {
		return nil
	}
	return s.cfg.Directory.Features()
}

// etagMatches implements If-None-Match for a strong ETag, including lists
// and "*". Weak tags (W/"...") match by their opaque value.
func etagMatches(header, etag string) bool {
	if header == "" || etag == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}
