// Package dashboard is the bridge's web dashboard: status, active calls,
// paired devices and pairing. It is plain server-rendered HTML with a
// little JavaScript, and it knows nothing about who may use it or where it
// is mounted: a Gate decides that (see gate.go).
//
// Today it only runs in the Home Assistant add-on, behind ingress.
package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

// Service is what the dashboard needs from the running bridge; the admin
// service implements it.
type Service interface {
	Status() admin.Status
	Devices() ([]admin.DeviceInfo, error)
	RenameDevice(id, name string) (admin.DeviceInfo, error)
	RemoveDevice(id string, keepCompanions bool) (admin.RemoveResult, error)
	CreatePairing(name string) (admin.PairingInfo, error)
	PairingState(code string) (admin.PairingState, error)
	RevokePairing(code string) error
	Calls() admin.CallsView
}

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Dashboard serves the pages.
type Dashboard struct {
	svc  Service
	gate Gate
	log  *slog.Logger
	tmpl *template.Template
	// csrf is sent with every form and checked on every POST. One token per
	// process: the gate has already decided who may see the pages.
	csrf string
	// PollInterval is how often a pairing wait checks the code.
	PollInterval time.Duration
	now          func() time.Time
}

// New creates the dashboard.
func New(svc Service, gate Gate, log *slog.Logger) *Dashboard {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		panic(err)
	}
	d := &Dashboard{
		svc: svc, gate: gate, log: log.With("component", "dashboard"),
		csrf: hex.EncodeToString(token), PollInterval: 300 * time.Millisecond, now: time.Now,
	}
	d.tmpl = template.Must(template.New("").Funcs(template.FuncMap{
		"since": func(l lang, t time.Time) string { return l.since(d.now().Sub(t)) },
		"clock": func(t time.Time) string { return t.Local().Format("15:04") },
		"date":  func(t time.Time) string { return t.Local().Format("02.01.2006 15:04") },
	}).ParseFS(templateFS, "templates/*.html"))
	return d
}

// Handler returns the dashboard behind its gate.
func (d *Dashboard) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", d.overview)
	mux.HandleFunc("POST /pair", d.post(d.pair))
	mux.HandleFunc("GET /pair/{code}/state", d.pairState)
	mux.HandleFunc("POST /pair/{code}/revoke", d.post(d.revoke))
	mux.HandleFunc("POST /devices/{id}/rename", d.post(d.rename))
	mux.HandleFunc("GET /devices/{id}/remove", d.confirmRemove)
	mux.HandleFunc("POST /devices/{id}/remove", d.post(d.remove))
	return d.gate.Guard(secureHeaders(mux))
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'self'; frame-ancestors 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// page is the data every template gets.
type page struct {
	L    lang
	Base string
	CSRF string
	Data any
}

func (d *Dashboard) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	p := page{L: langFor(r), Base: d.gate.BasePath(r), CSRF: d.csrf, Data: data}
	if err := d.tmpl.ExecuteTemplate(w, name, p); err != nil {
		d.log.Error("render", "template", name, "error", err)
	}
}

func (d *Dashboard) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, admin.ErrNotFound) {
		d.render(w, r, http.StatusNotFound, "message.html", msg{Title: langFor(r).T("notFound")})
		return
	}
	d.log.Error("request failed", "path", r.URL.Path, "error", err)
	d.render(w, r, http.StatusInternalServerError, "message.html", msg{Title: langFor(r).T("failed")})
}

// post checks the CSRF token (and, where the browser sends it, that the
// request comes from the same site) before a state-changing action.
func (d *Dashboard) post(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		site := r.Header.Get("Sec-Fetch-Site")
		token := r.PostFormValue("csrf")
		if (site != "" && site != "same-origin") || subtle.ConstantTimeCompare([]byte(token), []byte(d.csrf)) != 1 {
			d.render(w, r, http.StatusForbidden, "message.html", msg{Title: langFor(r).T("csrf")})
			return
		}
		next(w, r)
	}
}

func (d *Dashboard) redirectHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, d.gate.BasePath(r)+"/", http.StatusSeeOther)
}

type msg struct {
	Title  string
	Detail string
}

type overviewData struct {
	Status  admin.Status
	Devices []admin.DeviceInfo
	Calls   []callRow
}

type callRow struct {
	admin.CallInfo
	DeviceName string
}

func (d *Dashboard) overview(w http.ResponseWriter, r *http.Request) {
	devices, err := d.svc.Devices()
	if err != nil {
		d.fail(w, r, err)
		return
	}
	names := make(map[string]string, len(devices))
	for _, dev := range devices {
		names[dev.ID] = dev.Name
	}
	var calls []callRow
	for _, c := range d.svc.Calls().Active {
		calls = append(calls, callRow{CallInfo: c, DeviceName: names[c.DeviceID]})
	}
	d.render(w, r, http.StatusOK, "overview.html", overviewData{Status: d.svc.Status(), Devices: devices, Calls: calls})
}

type pairingData struct {
	Info admin.PairingInfo
	QR   template.URL
}

func (d *Dashboard) pair(w http.ResponseWriter, r *http.Request) {
	info, err := d.svc.CreatePairing(strings.TrimSpace(r.PostFormValue("name")))
	if err != nil {
		d.fail(w, r, err)
		return
	}
	code, err := qr.Encode(info.Link, qr.M)
	if err != nil {
		d.fail(w, r, err)
		return
	}
	code.Scale = 6
	data := pairingData{Info: info, QR: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()))}
	d.log.Info("pairing code created via dashboard", "expiresAt", info.ExpiresAt)
	d.render(w, r, http.StatusOK, "pair.html", data)
}

// pairState long-polls until the code is used or expired, or wait seconds
// (default 25, max 50) passed.
func (d *Dashboard) pairState(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	if wait <= 0 || wait > 50 {
		wait = 25
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(wait)*time.Second)
	defer cancel()
	ticker := time.NewTicker(d.PollInterval)
	defer ticker.Stop()
	for {
		st, err := d.svc.PairingState(code)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, admin.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]string{"error": http.StatusText(status)})
			return
		}
		if st.Used || st.Expired {
			writeJSON(w, http.StatusOK, st)
			return
		}
		select {
		case <-ctx.Done():
			writeJSON(w, http.StatusOK, st)
			return
		case <-ticker.C:
		}
	}
}

func (d *Dashboard) revoke(w http.ResponseWriter, r *http.Request) {
	if err := d.svc.RevokePairing(r.PathValue("code")); err != nil && !errors.Is(err, admin.ErrNotFound) {
		d.fail(w, r, err)
		return
	}
	d.redirectHome(w, r)
}

func (d *Dashboard) rename(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" {
		d.render(w, r, http.StatusBadRequest, "message.html", msg{Title: langFor(r).T("nameMissing")})
		return
	}
	if _, err := d.svc.RenameDevice(r.PathValue("id"), name); err != nil {
		d.fail(w, r, err)
		return
	}
	d.redirectHome(w, r)
}

type removeData struct {
	Device     admin.DeviceInfo
	Companions []admin.DeviceInfo
}

func (d *Dashboard) findDevice(id string) (removeData, error) {
	list, err := d.svc.Devices()
	if err != nil {
		return removeData{}, err
	}
	var out removeData
	found := false
	for _, dev := range list {
		switch {
		case dev.ID == id:
			out.Device, found = dev, true
		case dev.PairedBy == id:
			out.Companions = append(out.Companions, dev)
		}
	}
	if !found {
		return removeData{}, admin.ErrNotFound
	}
	return out, nil
}

func (d *Dashboard) confirmRemove(w http.ResponseWriter, r *http.Request) {
	data, err := d.findDevice(r.PathValue("id"))
	if err != nil {
		d.fail(w, r, err)
		return
	}
	d.render(w, r, http.StatusOK, "remove.html", data)
}

func (d *Dashboard) remove(w http.ResponseWriter, r *http.Request) {
	res, err := d.svc.RemoveDevice(r.PathValue("id"), r.PostFormValue("keepCompanions") == "1")
	if err != nil {
		d.fail(w, r, err)
		return
	}
	d.log.Info("devices removed via dashboard", "count", len(res.Removed))
	d.redirectHome(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
