package store

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"math/big"
	"path/filepath"
	"strings"
	"time"
)

// PairingCodeAlphabet avoids look-alikes (no 0 O 1 I): 32 symbols.
const PairingCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// PairingCodeLength is the number of characters in a pairing code: 16 of
// 32 symbols are 80 bits (signaling v2).
const PairingCodeLength = 16

// PairingTTL is how long a pairing code stays valid.
const PairingTTL = 10 * time.Minute

// ErrPairingInvalid covers unknown, expired and already used codes.
var ErrPairingInvalid = errors.New("pairing code invalid, expired or already used")

// PairingCode is a one-time code created by the pair command.
type PairingCode struct {
	Code string `json:"code"`
	Name string `json:"name,omitempty"`
	// ParentID is set for companion codes: the paired device that
	// requested the code (pair.companion.request).
	ParentID string `json:"parentId,omitempty"`
	// Platform restricts which platform may pair with the code (companion
	// codes: "watchos"). Empty allows any platform.
	Platform  string    `json:"platform,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// UsedCode records which device paired with a code, so the waiting pair
// command can report it.
type UsedCode struct {
	Code     string    `json:"code"`
	DeviceID string    `json:"deviceId"`
	UsedAt   time.Time `json:"usedAt"`
}

type pairingFile struct {
	Codes []PairingCode `json:"codes"`
	Used  []UsedCode    `json:"used,omitempty"`
}

// NormalizeCode is the canonical form of a typed code: upper case without
// the grouping hyphens and spaces (XXXX-XXXX-XXXX-XXXX).
func NormalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', ' ', '\t':
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(code)))
}

// RecordUse notes that deviceID paired with code; the record expires after
// PairingTTL.
func (p *Pairing) RecordUse(code, deviceID string, now time.Time) error {
	return withLock(p.path, func() error {
		var f pairingFile
		if err := readJSON(p.path, &f); err != nil {
			return err
		}
		f.Used = append(pruneUsed(f.Used, now), UsedCode{Code: NormalizeCode(code), DeviceID: deviceID, UsedAt: now.UTC()})
		return writeJSON(p.path, f)
	})
}

// UsedBy returns the device that paired with code, if any.
func (p *Pairing) UsedBy(code string, now time.Time) (UsedCode, bool, error) {
	var out UsedCode
	var found bool
	err := withLock(p.path, func() error {
		var f pairingFile
		if err := readJSON(p.path, &f); err != nil {
			return err
		}
		for _, u := range pruneUsed(f.Used, now) {
			if u.Code == NormalizeCode(code) {
				out, found = u, true
			}
		}
		return nil
	})
	return out, found, err
}

// Revoke drops an open code (pair command interrupted). It reports whether
// the code was still open.
func (p *Pairing) Revoke(code string) (bool, error) {
	var removed bool
	err := withLock(p.path, func() error {
		var f pairingFile
		if err := readJSON(p.path, &f); err != nil {
			return err
		}
		kept := f.Codes[:0]
		for _, pc := range f.Codes {
			if pc.Code == NormalizeCode(code) {
				removed = true
				continue
			}
			kept = append(kept, pc)
		}
		if !removed {
			return nil
		}
		f.Codes = kept
		return writeJSON(p.path, f)
	})
	return removed, err
}

func pruneUsed(used []UsedCode, now time.Time) []UsedCode {
	out := make([]UsedCode, 0, len(used))
	for _, u := range used {
		if now.Sub(u.UsedAt) < PairingTTL {
			out = append(out, u)
		}
	}
	return out
}

// Pairing is the file-backed store of pending pairing codes (pairing.json).
type Pairing struct {
	path string
}

// NewPairing returns the pairing store in dataDir.
func NewPairing(dataDir string) *Pairing {
	return &Pairing{path: filepath.Join(dataDir, "pairing.json")}
}

// Create adds a new code valid for PairingTTL and drops expired ones.
func (p *Pairing) Create(name string, now time.Time) (PairingCode, error) {
	code, err := newPairingCode()
	if err != nil {
		return PairingCode{}, err
	}
	pc := PairingCode{Code: code, Name: name, CreatedAt: now.UTC(), ExpiresAt: now.UTC().Add(PairingTTL)}
	err = withLock(p.path, func() error {
		var f pairingFile
		if err := readJSON(p.path, &f); err != nil {
			return err
		}
		f.Codes = append(pruneExpired(f.Codes, now), pc)
		return writeJSON(p.path, f)
	})
	return pc, err
}

// CreateCompanion adds a companion code for parentID that only platform may
// use. It replaces any open code of the same parent, so a device has at most
// one pending companion code.
func (p *Pairing) CreateCompanion(parentID, name, platform string, now time.Time) (PairingCode, error) {
	code, err := newPairingCode()
	if err != nil {
		return PairingCode{}, err
	}
	pc := PairingCode{Code: code, Name: name, ParentID: parentID, Platform: platform, CreatedAt: now.UTC(), ExpiresAt: now.UTC().Add(PairingTTL)}
	err = withLock(p.path, func() error {
		var f pairingFile
		if err := readJSON(p.path, &f); err != nil {
			return err
		}
		kept := pruneExpired(f.Codes, now)
		out := kept[:0]
		for _, existing := range kept {
			if existing.ParentID != parentID {
				out = append(out, existing)
			}
		}
		f.Codes = append(out, pc)
		return writeJSON(p.path, f)
	})
	return pc, err
}

// Pending returns the codes that are still valid at now.
func (p *Pairing) Pending(now time.Time) ([]PairingCode, error) {
	var out []PairingCode
	err := withLock(p.path, func() error {
		var f pairingFile
		if err := readJSON(p.path, &f); err != nil {
			return err
		}
		out = pruneExpired(f.Codes, now)
		return nil
	})
	return out, err
}

// RemoveByParent drops the open companion codes of parentID, e.g. when the
// parent device is removed.
func (p *Pairing) RemoveByParent(parentID string) error {
	return withLock(p.path, func() error {
		var f pairingFile
		if err := readJSON(p.path, &f); err != nil {
			return err
		}
		kept := f.Codes[:0]
		for _, pc := range f.Codes {
			if pc.ParentID != parentID {
				kept = append(kept, pc)
			}
		}
		if len(kept) == len(f.Codes) {
			return nil
		}
		f.Codes = kept
		return writeJSON(p.path, f)
	})
}

// Consume validates and removes a code. Comparison ignores case, hyphens
// and spaces and is constant-time per stored code.
func (p *Pairing) Consume(code string, now time.Time) (PairingCode, error) {
	normalized := []byte(NormalizeCode(code))
	var found PairingCode
	err := withLock(p.path, func() error {
		var f pairingFile
		if err := readJSON(p.path, &f); err != nil {
			return err
		}
		valid := pruneExpired(f.Codes, now)
		kept := valid[:0]
		for _, pc := range valid {
			if found.Code == "" && subtle.ConstantTimeCompare([]byte(pc.Code), normalized) == 1 {
				found = pc
				continue
			}
			kept = append(kept, pc)
		}
		if found.Code == "" {
			if len(kept) != len(f.Codes) {
				f.Codes = kept
				return errors.Join(ErrPairingInvalid, writeJSON(p.path, f))
			}
			return ErrPairingInvalid
		}
		f.Codes = kept
		return writeJSON(p.path, f)
	})
	return found, err
}

func pruneExpired(codes []PairingCode, now time.Time) []PairingCode {
	out := make([]PairingCode, 0, len(codes))
	for _, pc := range codes {
		if now.Before(pc.ExpiresAt) {
			out = append(out, pc)
		}
	}
	return out
}

func newPairingCode() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(PairingCodeAlphabet)))
	for range PairingCodeLength {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(PairingCodeAlphabet[n.Int64()])
	}
	return b.String(), nil
}
