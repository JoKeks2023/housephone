// Package auth implements device credentials: random secrets, stored as
// SHA-256 hashes and presented as "Bearer <deviceId>.<secret>".
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
)

// SecretBytes is the entropy of a device secret.
const SecretBytes = 32

// NewSecret returns 32 random bytes, base64url without padding (43 chars).
func NewSecret() (string, error) {
	b := make([]byte, SecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashSecret returns the hex SHA-256 of a secret for storage.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// VerifySecret compares a presented secret with a stored hash in constant time.
func VerifySecret(secret, storedHash string) bool {
	want, err := hex.DecodeString(storedHash)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

// ParseBearer extracts device ID and secret from an Authorization header.
// The device ID must be a lower-case UUID; the secret must be non-empty
// base64url.
func ParseBearer(header string) (deviceID, secret string, ok bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	deviceID, secret, found := strings.Cut(token, ".")
	if !found || secret == "" {
		return "", "", false
	}
	parsed, err := uuid.Parse(deviceID)
	if err != nil || parsed.String() != deviceID {
		return "", "", false
	}
	if _, err := base64.RawURLEncoding.DecodeString(secret); err != nil {
		return "", "", false
	}
	return deviceID, secret, true
}
