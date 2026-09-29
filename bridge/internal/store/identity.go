package store

import (
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Identity is the stable identity of this bridge (bridge.json).
type Identity struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
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
