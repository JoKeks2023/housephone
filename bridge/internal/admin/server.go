package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxSocketPath is the smallest Unix socket path limit (macOS: 104 bytes
// including the terminating zero; Linux allows 108).
const maxSocketPath = 104

// Server serves the admin API on a Unix socket.
type Server struct {
	svc      Service
	path     string
	log      *slog.Logger
	listener net.Listener
	http     *http.Server
	// PollInterval is how often a pairing wait checks the code.
	PollInterval time.Duration
}

// Listen creates the socket (0600) in dataDir. A stale socket from a crash
// is replaced; a socket another bridge still serves is an error.
func Listen(dataDir string, svc Service, log *slog.Logger) (*Server, error) {
	path := filepath.Join(dataDir, SocketName)
	if len(path) >= maxSocketPath {
		return nil, fmt.Errorf("admin socket %s: Pfad ist länger als %d Zeichen (Grenze des Betriebssystems) – kürzeres bridge.dataDir wählen", path, maxSocketPath-1)
	}
	if _, err := os.Stat(path); err == nil {
		if c, err := net.DialTimeout("unix", path, 500*time.Millisecond); err == nil {
			c.Close()
			return nil, fmt.Errorf("admin socket %s wird schon benutzt – läuft die Bridge bereits?", path)
		}
		_ = os.Remove(path)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("admin socket %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, fmt.Errorf("admin socket %s: %w", path, err)
	}
	s := &Server{svc: svc, path: path, log: log.With("component", "admin"), listener: l, PollInterval: 300 * time.Millisecond}
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	return s, nil
}

// Path is the socket path.
func (s *Server) Path() string { return s.path }

// Serve blocks until Close.
func (s *Server) Serve() error {
	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close stops serving and removes the socket.
func (s *Server) Close(ctx context.Context) {
	_ = s.http.Shutdown(ctx)
	_ = os.Remove(s.path)
}

// Handler routes the admin API (exported for tests).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, s.svc.Status()) })
	mux.HandleFunc("GET /v1/profiles", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, s.svc.Profiles()) })
	mux.HandleFunc("POST /v1/devices/{id}/move", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Profile string `json:"profile"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.Profile) == "" {
			writeError(w, http.StatusBadRequest, "Profil fehlt")
			return
		}
		res, err := s.svc.MoveDevice(r.PathValue("id"), strings.TrimSpace(req.Profile))
		s.reply(w, res, err)
	})
	mux.HandleFunc("GET /v1/devices", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.svc.Devices()
		s.reply(w, list, err)
	})
	mux.HandleFunc("POST /v1/devices/{id}/rename", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			writeError(w, http.StatusBadRequest, "Name fehlt")
			return
		}
		dev, err := s.svc.RenameDevice(r.PathValue("id"), req.Name)
		s.reply(w, dev, err)
	})
	mux.HandleFunc("POST /v1/devices/{id}/promote", func(w http.ResponseWriter, r *http.Request) {
		dev, err := s.svc.PromoteDevice(r.PathValue("id"))
		s.reply(w, dev, err)
	})
	mux.HandleFunc("POST /v1/devices/{id}/demote", func(w http.ResponseWriter, r *http.Request) {
		dev, err := s.svc.DemoteDevice(r.PathValue("id"))
		s.reply(w, dev, err)
	})
	mux.HandleFunc("DELETE /v1/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.svc.RemoveDevice(r.PathValue("id"), r.URL.Query().Get("keepCompanions") == "1")
		s.reply(w, res, err)
	})
	mux.HandleFunc("POST /v1/pairing", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			Profile string `json:"profile"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
		info, err := s.svc.CreatePairing(req.Name, strings.TrimSpace(req.Profile))
		s.reply(w, info, err)
	})
	mux.HandleFunc("GET /v1/pairing/{code}", s.waitPairing)
	mux.HandleFunc("DELETE /v1/pairing/{code}", func(w http.ResponseWriter, r *http.Request) {
		s.reply(w, map[string]bool{"revoked": true}, s.svc.RevokePairing(r.PathValue("code")))
	})
	mux.HandleFunc("GET /v1/lan-pairings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.svc.LanPairings())
	})
	mux.HandleFunc("POST /v1/lan-pairings/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Profile string `json:"profile"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
		dev, err := s.svc.ApproveLanPairing(r.PathValue("id"), strings.TrimSpace(req.Profile))
		s.reply(w, dev, err)
	})
	mux.HandleFunc("POST /v1/lan-pairings/{id}/deny", func(w http.ResponseWriter, r *http.Request) {
		s.reply(w, map[string]bool{"denied": true}, s.svc.DenyLanPairing(r.PathValue("id")))
	})
	mux.HandleFunc("GET /v1/calls", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, s.svc.Calls()) })
	mux.HandleFunc("GET /v1/stats", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, s.svc.Stats()) })
	mux.HandleFunc("GET /v1/logs", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		writeJSON(w, http.StatusOK, s.svc.Logs(after))
	})
	mux.HandleFunc("GET /v1/selftest", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		writeJSON(w, http.StatusOK, s.svc.SelfTest(ctx))
	})
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, s.svc.Config()) })
	return mux
}

// waitPairing long-polls until the code is used or expired, or wait
// seconds (default 25, max 60) passed.
func (s *Server) waitPairing(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	if wait <= 0 || wait > 60 {
		wait = 25
	}
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	ticker := time.NewTicker(s.PollInterval)
	defer ticker.Stop()
	for {
		st, err := s.svc.PairingState(code)
		if err != nil || st.Used || st.Expired || !time.Now().Before(deadline) {
			s.reply(w, st, err)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) reply(w http.ResponseWriter, v any, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, v)
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "nicht gefunden")
	case errors.Is(err, ErrUnknownProfile):
		writeError(w, http.StatusBadRequest, ErrUnknownProfile.Error())
	case errors.Is(err, ErrNotAllowed):
		writeError(w, http.StatusBadRequest, ErrNotAllowed.Error())
	default:
		s.log.Error("admin request failed", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
