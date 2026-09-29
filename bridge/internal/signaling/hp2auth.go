package signaling

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Signaling v2 (ADR-0004): every request except POST /v1/pair and the bare
// health check carries "Authorization: HP2 …", signed with the device key.
// Every answer to such a request carries the bridge's HP2-Bridge signature,
// and bodies of authenticated answers are sealed.

// maxNoncesPerDevice bounds the nonce cache of one device; honest devices
// send far fewer requests within hp2.NonceWindow.
const maxNoncesPerDevice = 4096

// nonceCache remembers the nonces of accepted requests per device for
// hp2.NonceWindow, so a captured request cannot be replayed within the
// clock window.
type nonceCache struct {
	mu      sync.Mutex
	devices map[string]map[string]time.Time
}

func newNonceCache() *nonceCache {
	return &nonceCache{devices: map[string]map[string]time.Time{}}
}

// use records nonce for deviceID and reports false if it was seen within
// the window, or if the device's cache is full of live entries.
func (c *nonceCache) use(deviceID, nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := c.devices[deviceID]
	if seen == nil {
		seen = map[string]time.Time{}
		c.devices[deviceID] = seen
	}
	if expiry, ok := seen[nonce]; ok && now.Before(expiry) {
		return false
	}
	if len(seen) >= maxNoncesPerDevice {
		for n, expiry := range seen {
			if !now.Before(expiry) {
				delete(seen, n)
			}
		}
		if len(seen) >= maxNoncesPerDevice {
			return false
		}
	}
	seen[nonce] = now.Add(hp2.NonceWindow)
	return true
}

// forget drops the cache of a removed device.
func (c *nonceCache) forget(deviceID string) {
	c.mu.Lock()
	delete(c.devices, deviceID)
	c.mu.Unlock()
}

// authResult is the outcome of checking a request.
type authResult struct {
	dev store.Device
	// sess is set whenever the Authorization header parsed, so even a
	// rejection can be signed.
	sess *hp2.ServerSession
	// code is "" for an authenticated request, else the error code.
	code string
}

// checkRequest verifies the HP2 signature, the clock window and the nonce
// of a request. The nonce is only recorded for valid signatures, so
// forged requests cannot fill the cache.
func (s *Server) checkRequest(r *http.Request, body []byte) authResult {
	res := authResult{code: protocol.ErrorUnauthorized}
	h, err := hp2.ParseAuthorization(r.Header.Get("Authorization"))
	if err != nil {
		return res
	}
	sess, err := hp2.Accept(s.cfg.BridgeID, h)
	if err != nil {
		return res
	}
	res.sess = sess
	dev, err := s.cfg.Devices.Get(h.DeviceID)
	if err != nil {
		return res
	}
	pub, err := hp2.ParseDevicePublicKey(dev.PublicKey)
	if err != nil {
		return res
	}
	if !hp2.VerifyRequest(pub, h, r.Method, r.RequestURI, s.cfg.BridgeID, body) {
		return res
	}
	now := s.cfg.Now()
	if !hp2.WithinClockSkew(h.TS, now) {
		res.code = protocol.ErrorClockSkew
		return res
	}
	if !s.nonces.use(h.DeviceID, h.Nonce, now) {
		return res
	}
	res.dev = dev
	res.code = ""
	return res
}

// rejectAuth answers 401 {"code":…}; signed when the header parsed.
func (s *Server) rejectAuth(w http.ResponseWriter, r *http.Request, res authResult) {
	s.warnClient("rejected request authentication", s.clientIP(r), "path", r.URL.Path, "reason", res.code)
	s.writeSignedError(w, res.sess, http.StatusUnauthorized, protocol.Error{Code: res.code, Message: "unauthorized"})
}

// writeSignedError answers with a plain JSON error (no session keys are
// trusted yet), signed when sess is set.
func (s *Server) writeSignedError(w http.ResponseWriter, sess *hp2.ServerSession, status int, e protocol.Error) {
	body, _ := json.Marshal(e)
	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if sess != nil {
		w.Header().Set(hp2.BridgeHeader, sess.SignAnswer(s.cfg.Identity, status, body))
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// sessionFromHeader returns the session of a well-formed Authorization
// header without checking the request, so an early error (body too large)
// can still be signed.
func (s *Server) sessionFromHeader(r *http.Request) *hp2.ServerSession {
	h, err := hp2.ParseAuthorization(r.Header.Get("Authorization"))
	if err != nil {
		return nil
	}
	sess, err := hp2.Accept(s.cfg.BridgeID, h)
	if err != nil {
		return nil
	}
	return sess
}

// authedHandler serves an authenticated request; what it writes is sealed.
type authedHandler func(w http.ResponseWriter, r *http.Request, dev store.Device)

// authed wraps an HTTPS endpoint with HP2 authentication and sealing.
func (s *Server) authed(h authedHandler) http.HandlerFunc {
	return withDeadline(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPBody))
		if err != nil {
			s.writeSignedError(w, s.sessionFromHeader(r), http.StatusRequestEntityTooLarge, protocol.Error{Code: protocol.ErrorBadRequest, Message: "request body too large"})
			return
		}
		res := s.checkRequest(r, body)
		if res.code != "" {
			s.rejectAuth(w, r, res)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := &recorder{header: http.Header{}, status: http.StatusOK}
		h(rec, r, res.dev)
		s.writeSealed(w, rec, res.sess)
	})
}

// writeSealed sends a recorded answer: body sealed with the bridge →
// device key, HP2-Bridge signature over status and the body as sent.
func (s *Server) writeSealed(w http.ResponseWriter, rec *recorder, sess *hp2.ServerSession) {
	for k, vs := range rec.header {
		w.Header()[k] = vs
	}
	var sent []byte
	if rec.body.Len() > 0 && rec.status != http.StatusNoContent && rec.status != http.StatusNotModified {
		sealed, err := hp2.SealBody(sess.Keys.BridgeToDevice, rec.body.Bytes())
		if err != nil {
			s.log.Error("sealing answer failed", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		sent = sealed
		w.Header().Set("Content-Type", hp2.SealedContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(sent)))
	} else {
		w.Header().Del("Content-Type")
	}
	w.Header().Set(hp2.BridgeHeader, sess.SignAnswer(s.cfg.Identity, rec.status, sent))
	w.WriteHeader(rec.status)
	if len(sent) > 0 {
		_, _ = w.Write(sent)
	}
}

// recorder buffers an answer so it can be sealed and signed as a whole.
type recorder struct {
	header      http.Header
	status      int
	body        bytes.Buffer
	wroteHeader bool
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
}

func (r *recorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.body.Write(b)
}
