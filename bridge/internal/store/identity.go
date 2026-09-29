package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Identity is the stable identity of this bridge (bridge.json).
type Identity struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

// identityKeyFile holds the Ed25519 seed of the bridge (signaling v2).
type identityKeyFile struct {
	Version int    `json:"version"`
	Seed    []byte `json:"ed25519Seed"`
}

// IdentityKeyPath is where the bridge's signing key lives.
func IdentityKeyPath(dataDir string) string { return filepath.Join(dataDir, "identity.key") }

// LoadOrCreateIdentityKey returns the 32-byte Ed25519 seed of the bridge
// from identity.key (0600), calling newSeed to create it on first use. The
// devices pin the matching public key; losing the file means pairing all
// devices again.
func LoadOrCreateIdentityKey(dataDir string, newSeed func() ([]byte, error)) ([]byte, error) {
	path := IdentityKeyPath(dataDir)
	var f identityKeyFile
	err := withLock(path, func() error {
		if err := readJSON(path, &f); err != nil {
			return err
		}
		if len(f.Seed) != 0 {
			if len(f.Seed) != 32 {
				return fmt.Errorf("%s: seed must be 32 bytes", path)
			}
			return nil
		}
		seed, err := newSeed()
		if err != nil {
			return err
		}
		f = identityKeyFile{Version: 1, Seed: seed}
		return writeJSON(path, f)
	})
	return f.Seed, err
}

// LoadIdentityKey returns the existing seed without creating one.
func LoadIdentityKey(dataDir string) ([]byte, error) {
	path := IdentityKeyPath(dataDir)
	var f identityKeyFile
	err := withLock(path, func() error { return readJSON(path, &f) })
	if err == nil && len(f.Seed) != 32 {
		err = errors.New("no bridge identity yet (start the bridge once)")
	}
	return f.Seed, err
}

// LoadOrCreateIdentity returns the bridge identity, creating it on first use.
func LoadOrCreateIdentity(dataDir string) (Identity, error) {
	path := filepath.Join(dataDir, "bridge.json")
	var id Identity
	err := withLock(path, func() error {
		if err := readJSON(path, &id); err != nil {
			return err
		}
		if id.ID != "" {
			return nil
		}
		id = Identity{ID: uuid.NewString(), CreatedAt: time.Now().UTC()}
		return writeJSON(path, id)
	})
	return id, err
}
