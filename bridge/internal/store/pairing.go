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

// PairingCodeLength is the number of characters in a pairing code.
const PairingCodeLength = 10

// PairingTTL is how long a pairing code stays valid.
const PairingTTL = 10 * time.Minute

// ErrPairingInvalid covers unknown, expired and already used codes.
var ErrPairingInvalid = errors.New("pairing code invalid, expired or already used")

// PairingCode is a one-time code created by the pair command.
type PairingCode struct {
	Code      string    `json:"code"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type pairingFile struct {
	Codes []PairingCode `json:"codes"`
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

// Consume validates and removes a code. Comparison is case-insensitive and
// constant-time per stored code.
func (p *Pairing) Consume(code string, now time.Time) (PairingCode, error) {
	normalized := []byte(strings.ToUpper(strings.TrimSpace(code)))
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
