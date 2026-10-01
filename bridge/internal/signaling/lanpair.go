package signaling

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Pairing in the home network without a QR code (ADR-0007): the device
// finds the bridge via Bonjour, both sides show a confirmation code (SAS)
// and an admin approves the request in the TUI, the CLI or the dashboard.
// Only the private listener serves it; requests live in memory.

// Limits of LAN pairing.
const (
	// DefaultLanPairTTL is how long a request waits for the admin.
	DefaultLanPairTTL = 2 * time.Minute
	// lanResultTTL keeps an approval or denial for the device to collect.
	lanResultTTL = time.Minute
	// lanMaxOpen bounds the requests waiting at the same time.
	lanMaxOpen = 5
	// lanStartsPerSource and lanStartsWindow bound new requests per
	// address (or /64).
	lanStartsPerSource = 5
	lanStartsWindow    = 10 * time.Minute
	// lanStartsGlobal bounds new requests per hour overall.
	lanStartsGlobal = 30
	// lanMaxWait bounds a long poll of GET /v1/pair/lan/{id}.
	lanMaxWait = 25 * time.Second
)

// ErrLanPairingNotFound: no such request waiting for approval.
var ErrLanPairingNotFound = errors.New("lan pairing request not found or expired")

type lanStatus int

const (
	// lanOffered: step 2 answered, waiting for the reveal.
	lanOffered lanStatus = iota
	// lanPending: revealed, waiting for the admin.
	lanPending
	lanApproved
	lanDenied
)

type lanEntry struct {
	id        string
	source    string // limiterKey of the client
	ip        string
	start     protocol.LanPairStart
	name      string
	createdAt time.Time
	expiresAt time.Time
	ephemeral *ecdh.PrivateKey
	offer     protocol.LanPairOffer
	sas       string
	status    lanStatus
	// hash and sealKey exist once revealed; the key is dropped after the
	// decision.
	hash    []byte
	sealKey []byte
	// sealed is the approval for the device (base64url).
	sealed string
	// changed is closed and replaced on every status change.
	changed chan struct{}
}

func (e *lanEntry) notify() {
	close(e.changed)
	e.changed = make(chan struct{})
}

// LanPairingRequest is a request waiting for an admin's decision.
type LanPairingRequest struct {
	ID         string
	DeviceName string
	Platform   string
	Model      string
	IP         string
	// SAS is the confirmation code the device shows.
	SAS            string
	KeyFingerprint string
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

type lanPairing struct {
	mu      sync.Mutex
	entries map[string]*lanEntry
	starts  *rateLimiter
	global  *rateLimiter
}

func newLanPairing(now func() time.Time) *lanPairing {
	return &lanPairing{
		entries: map[string]*lanEntry{},
		starts:  newRateLimiter(lanStartsPerSource, lanStartsWindow, now),
		global:  newRateLimiter(lanStartsGlobal, time.Hour, now),
	}
}

// sweepLanLocked drops expired requests and wakes their pollers. s.lan.mu held.
func (s *Server) sweepLanLocked(now time.Time) {
	for id, e := range s.lan.entries {
		if !now.Before(e.expiresAt) {
			delete(s.lan.entries, id)
			e.notify()
		}
	}
}

func (s *Server) lanTTL() time.Duration {
	if s.cfg.LanPairTTL > 0 {
		return s.cfg.LanPairTTL
	}
	return DefaultLanPairTTL
}

var (
	errLanBadRequest = &pairError{protocol.ErrorBadRequest, "deviceName, platform (ios), publicKey and commitment are required", http.StatusBadRequest}
	errLanUnknown    = &pairError{protocol.ErrorPairingInvalid, "Die Kopplungsanfrage ist unbekannt oder abgelaufen", http.StatusNotFound}
	errLanBusy       = &pairError{protocol.ErrorPairingRateLimited, "Gerade warten zu viele Kopplungsanfragen, bitte später erneut probieren", http.StatusTooManyRequests}
)

// httpLanStart handles POST /v1/pair/lan (step 1 → 2).
func (s *Server) httpLanStart(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	var req protocol.LanPairStart
	decodeErr := decodeBody(w, r, &req)
	offer, err := s.lanStart(req, decodeErr, ip)
	if err != nil {
		writeJSON(w, err.status, protocol.Error{Code: err.code, Message: err.message})
		return
	}
	writeJSON(w, http.StatusOK, offer)
}

func (s *Server) lanStart(req protocol.LanPairStart, decodeErr error, ip string) (protocol.LanPairOffer, *pairError) {
	if s.lan.starts.blocked(ip) || s.lan.global.blocked("lan") {
		s.warnClient("lan pairing rate limited", ip)
		return protocol.LanPairOffer{}, errPairRateLimited
	}
	name := sanitizeName(req.DeviceName)
	if decodeErr != nil || name == "" || req.Platform != protocol.PlatformIOS {
		return protocol.LanPairOffer{}, errLanBadRequest
	}
	if _, err := hp2.ParseDevicePublicKey(req.PublicKey); err != nil {
		return protocol.LanPairOffer{}, errLanBadRequest
	}
	if c, err := hp2.DecodeB64(req.Commitment); err != nil || len(c) != 32 {
		return protocol.LanPairOffer{}, errLanBadRequest
	}
	s.lan.starts.fail(ip)
	s.lan.global.fail("lan")

	eph, err := hp2.NewEphemeral()
	if err != nil {
		return protocol.LanPairOffer{}, errPairInternal
	}
	idRaw := make([]byte, 16)
	nonce := make([]byte, hp2.NonceSize)
	if _, err := rand.Read(idRaw); err != nil {
		return protocol.LanPairOffer{}, errPairInternal
	}
	if _, err := rand.Read(nonce); err != nil {
		return protocol.LanPairOffer{}, errPairInternal
	}
	now := s.cfg.Now()
	e := &lanEntry{
		id:        hp2.B64(idRaw),
		source:    limiterKey(ip),
		ip:        ip,
		start:     req,
		name:      name,
		createdAt: now,
		expiresAt: now.Add(s.lanTTL()),
		ephemeral: eph,
		changed:   make(chan struct{}),
	}
	bridgePub := hp2.B64(s.cfg.Identity.PublicKey())
	e.offer = protocol.LanPairOffer{
		PairingID:       e.id,
		BridgeID:        s.cfg.BridgeID,
		BridgeName:      s.cfg.BridgeName,
		BridgePublicKey: bridgePub,
		BridgeEphemeral: hp2.B64(eph.PublicKey().Bytes()),
		BridgeNonce:     hp2.B64(nonce),
		ExpiresAt:       e.expiresAt.UTC(),
	}
	e.offer.Signature = hp2.B64(s.cfg.Identity.Sign(hp2.LanOfferMessage(e.id, s.cfg.BridgeID, req.PublicKey,
		req.Commitment, bridgePub, e.offer.BridgeEphemeral, e.offer.BridgeNonce)))

	s.lan.mu.Lock()
	defer s.lan.mu.Unlock()
	s.sweepLanLocked(now)
	open := 0
	for id, other := range s.lan.entries {
		if other.status > lanPending {
			continue
		}
		// A retry from the same address replaces its earlier request.
		if other.source == e.source {
			delete(s.lan.entries, id)
			other.notify()
			continue
		}
		open++
	}
	if open >= lanMaxOpen {
		s.warnClient("lan pairing: too many open requests", ip)
		return protocol.LanPairOffer{}, errLanBusy
	}
	s.lan.entries[e.id] = e
	return e.offer, nil
}

// httpLanReveal handles POST /v1/pair/lan/{id}/reveal (step 3).
func (s *Server) httpLanReveal(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	var req protocol.LanPairReveal
	if err := decodeBody(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Error{Code: protocol.ErrorBadRequest, Message: "deviceEphemeral, deviceNonce and proof are required"})
		return
	}
	state, err := s.lanReveal(r.PathValue("id"), req, ip)
	if err != nil {
		writeJSON(w, err.status, protocol.Error{Code: err.code, Message: err.message})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) lanReveal(id string, req protocol.LanPairReveal, ip string) (protocol.LanPairState, *pairError) {
	now := s.cfg.Now()
	s.lan.mu.Lock()
	defer s.lan.mu.Unlock()
	s.sweepLanLocked(now)
	e := s.lan.entries[id]
	if e == nil || e.status != lanOffered || e.source != limiterKey(ip) {
		return protocol.LanPairState{}, errLanUnknown
	}
	// Any mismatch ends the request: a second reveal must not be possible.
	reject := func(reason string) (protocol.LanPairState, *pairError) {
		delete(s.lan.entries, id)
		e.notify()
		s.lan.starts.fail(ip)
		s.warnClient("lan pairing rejected", ip, "reason", reason)
		return protocol.LanPairState{}, errPairInvalid
	}
	if !hp2.CheckLanCommitment(e.start.Commitment, e.start.PublicKey, req.DeviceEphemeral, req.DeviceNonce) {
		return reject("commitment")
	}
	if _, err := hp2.DecodeB64(req.DeviceNonce); err != nil {
		return reject("nonce")
	}
	peer, err := hp2.ParseEphemeral(req.DeviceEphemeral)
	if err != nil {
		return reject("ephemeral key")
	}
	t := hp2.LanTranscript{
		PairingID: e.id, BridgeID: e.offer.BridgeID, PublicKey: e.start.PublicKey, Commitment: e.start.Commitment,
		BridgePublicKey: e.offer.BridgePublicKey, BridgeEphemeral: e.offer.BridgeEphemeral, BridgeNonce: e.offer.BridgeNonce,
		DeviceEphemeral: req.DeviceEphemeral, DeviceNonce: req.DeviceNonce,
	}
	hash := t.Hash()
	pub, _ := hp2.ParseDevicePublicKey(e.start.PublicKey)
	proof, err := hp2.DecodeB64(req.Proof)
	if err != nil || !hp2.VerifyDeviceSignature(pub, hp2.LanProofMessage(hash), proof) {
		return reject("proof")
	}
	shared, err := hp2.LanShared(e.ephemeral, peer)
	if err != nil {
		return reject("shared secret")
	}
	secrets, err := hp2.DeriveLanSecrets(shared, hash)
	if err != nil {
		return protocol.LanPairState{}, errPairInternal
	}
	e.sas = secrets.SAS
	e.status = lanPending
	e.ephemeral = nil
	e.hash = hash
	e.sealKey = secrets.SealKey
	e.notify()
	s.log.Info("LAN pairing request waiting for approval", "request", shortID(e.id), "name", e.name,
		"model", sanitizeName(e.start.Model), "key", hp2.KeyFingerprint(e.start.PublicKey), "ip", ip)
	return protocol.LanPairState{Status: protocol.LanPairPending, ExpiresAt: e.expiresAt.UTC()}, nil
}

// httpLanState handles GET /v1/pair/lan/{id}?wait=<seconds>: the state of
// a request, as a long poll while it waits for the admin.
func (s *Server) httpLanState(w http.ResponseWriter, r *http.Request) {
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	deadline := time.Now().Add(min(time.Duration(max(wait, 0))*time.Second, lanMaxWait))
	id := r.PathValue("id")
	source := limiterKey(s.clientIP(r))
	for {
		s.lan.mu.Lock()
		s.sweepLanLocked(s.cfg.Now())
		e := s.lan.entries[id]
		if e == nil || e.source != source || e.status == lanOffered {
			s.lan.mu.Unlock()
			writeJSON(w, http.StatusOK, protocol.LanPairState{Status: protocol.LanPairExpired})
			return
		}
		state := lanState(e)
		changed := e.changed
		expires := e.expiresAt
		s.lan.mu.Unlock()
		remaining := time.Until(deadline)
		if state.Status != protocol.LanPairPending || remaining <= 0 {
			writeJSON(w, http.StatusOK, state)
			return
		}
		// Wake up at the deadline, on a change, or when the request expires.
		untilExpiry := expires.Sub(s.cfg.Now())
		timer := time.NewTimer(max(min(remaining, untilExpiry), 0))
		select {
		case <-changed:
		case <-timer.C:
		case <-r.Context().Done():
			timer.Stop()
			return
		}
		timer.Stop()
	}
}

func lanState(e *lanEntry) protocol.LanPairState {
	switch e.status {
	case lanApproved:
		return protocol.LanPairState{Status: protocol.LanPairApproved, Sealed: e.sealed}
	case lanDenied:
		return protocol.LanPairState{Status: protocol.LanPairDenied}
	default:
		return protocol.LanPairState{Status: protocol.LanPairPending, ExpiresAt: e.expiresAt.UTC()}
	}
}

// LanPairings lists the requests waiting for an admin, oldest first.
func (s *Server) LanPairings() []LanPairingRequest {
	now := s.cfg.Now()
	s.lan.mu.Lock()
	defer s.lan.mu.Unlock()
	s.sweepLanLocked(now)
	var out []LanPairingRequest
	for _, e := range s.lan.entries {
		if e.status != lanPending {
			continue
		}
		out = append(out, LanPairingRequest{
			ID: e.id, DeviceName: e.name, Platform: e.start.Platform, Model: sanitizeName(e.start.Model), IP: e.ip,
			SAS: e.sas, KeyFingerprint: hp2.KeyFingerprint(e.start.PublicKey), CreatedAt: e.createdAt, ExpiresAt: e.expiresAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// ApproveLanPairing pairs the device of a waiting request. The caller has
// shown the admin the SAS; ApproveLanPairing does not check it again.
func (s *Server) ApproveLanPairing(id string) (store.Device, error) {
	now := s.cfg.Now()
	dev, err := s.approveLan(id, now)
	if err == nil {
		s.broadcastDevicePaired(dev, now)
	}
	return dev, err
}

func (s *Server) approveLan(id string, now time.Time) (store.Device, error) {
	s.lan.mu.Lock()
	defer s.lan.mu.Unlock()
	s.sweepLanLocked(now)
	e := s.lan.entries[id]
	if e == nil || e.status != lanPending {
		return store.Device{}, ErrLanPairingNotFound
	}
	dev := store.Device{
		ID:        uuid.NewString(),
		Name:      e.name,
		Platform:  e.start.Platform,
		Model:     sanitizeName(e.start.Model),
		PublicKey: e.start.PublicKey,
		CreatedAt: now.UTC(),
	}
	hash := e.hash
	approval := protocol.LanPairApproval{
		DeviceID:   dev.ID,
		BridgeID:   s.cfg.BridgeID,
		BridgeName: s.cfg.BridgeName,
		PublicURL:  s.cfg.PublicURL,
		LanURL:     s.cfg.LanURL,
	}
	approval.Signature = hp2.B64(s.cfg.Identity.Sign(hp2.LanApprovedMessage(hash, dev.ID, s.cfg.BridgeID, s.cfg.PublicURL, s.cfg.LanURL)))
	plain, err := json.Marshal(approval)
	if err != nil {
		return store.Device{}, err
	}
	sealed, err := hp2.SealLan(e.sealKey, plain)
	if err != nil {
		return store.Device{}, err
	}
	if err := s.cfg.Devices.Add(dev); err != nil {
		return store.Device{}, err
	}
	e.status = lanApproved
	e.sealed = hp2.B64(sealed)
	e.sealKey = nil
	e.expiresAt = now.Add(lanResultTTL)
	e.notify()
	s.log.Info("device paired in the home network", "device", dev.ID, "name", dev.Name, "model", dev.Model,
		"key", hp2.KeyFingerprint(dev.PublicKey), "ip", e.ip)
	return dev, nil
}

// DenyLanPairing refuses a waiting request.
func (s *Server) DenyLanPairing(id string) error {
	now := s.cfg.Now()
	s.lan.mu.Lock()
	defer s.lan.mu.Unlock()
	s.sweepLanLocked(now)
	e := s.lan.entries[id]
	if e == nil || e.status != lanPending {
		return ErrLanPairingNotFound
	}
	e.status = lanDenied
	e.sealKey = nil
	e.expiresAt = now.Add(lanResultTTL)
	e.notify()
	s.log.Info("LAN pairing request denied", "request", shortID(e.id), "name", e.name, "ip", e.ip)
	return nil
}

func shortID(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// homeNetworkOnly answers the pairing routes on the public listener.
func (s *Server) homeNetworkOnly(w http.ResponseWriter, r *http.Request) {
	s.writeSignedError(w, s.sessionFromHeader(r), http.StatusForbidden, protocol.Error{Code: protocol.ErrorHomeNetworkRequired, Message: "pairing is only possible in the home network"})
}
