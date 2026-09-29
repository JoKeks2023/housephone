package hp2

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// ErrIntegrity reports a sealed frame or body that fails authentication,
// arrives out of order or is not a sealed frame at all. The connection must
// be closed (WebSocket close code 4002).
var ErrIntegrity = errors.New("hp2: integrity check failed")

// SessionKeys are the two directions of one authenticated request.
type SessionKeys struct {
	// DeviceToBridge seals what the device sends.
	DeviceToBridge []byte
	// BridgeToDevice seals what the bridge sends.
	BridgeToDevice []byte
}

// DeriveKeys computes the session keys from X25519(own, peer) with
// HKDF-SHA256(salt = request nonce, info = KeysInfo, 64 bytes). Both sides
// get the same keys: the first half for device → bridge, the second half
// for bridge → device.
func DeriveKeys(own *ecdh.PrivateKey, peerPublic, nonce []byte, info string) (SessionKeys, error) {
	peer, err := ecdh.X25519().NewPublicKey(peerPublic)
	if err != nil {
		return SessionKeys{}, ErrMalformed
	}
	// ECDH rejects low-order points (all-zero shared secret).
	shared, err := own.ECDH(peer)
	if err != nil {
		return SessionKeys{}, ErrMalformed
	}
	okm, err := hkdf.Key(sha256.New, shared, nonce, info, 64)
	if err != nil {
		return SessionKeys{}, err
	}
	return SessionKeys{DeviceToBridge: okm[:32], BridgeToDevice: okm[32:]}, nil
}

// nonceFor is the 12-byte AEAD nonce: four zero bytes and the big-endian
// counter.
func nonceFor(counter uint64) []byte {
	n := make([]byte, chacha20poly1305.NonceSize)
	binary.BigEndian.PutUint64(n[4:], counter)
	return n
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, errors.New("hp2: key must be 32 bytes")
	}
	return chacha20poly1305.New(key)
}

// Sealer seals the frames of one direction with an increasing counter.
type Sealer struct {
	mu      sync.Mutex
	aead    cipher.AEAD
	counter uint64
}

// NewSealer starts at counter 0.
func NewSealer(key []byte) (*Sealer, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts the next frame. Frames must be sent in the order they were
// sealed; callers that write concurrently hold their own lock across Seal
// and the write.
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counter == math.MaxUint64 {
		return nil, errors.New("hp2: frame counter exhausted")
	}
	out := s.aead.Seal(nil, nonceFor(s.counter), plaintext, aad)
	s.counter++
	return out, nil
}

// Opener opens the frames of one direction; each frame must carry the next
// counter value, so dropped, repeated and reordered frames fail.
type Opener struct {
	mu      sync.Mutex
	aead    cipher.AEAD
	counter uint64
	failed  bool
}

// NewOpener expects counter 0 first.
func NewOpener(key []byte) (*Opener, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return &Opener{aead: aead}, nil
}

// Open decrypts the next frame. After a failure every further call fails.
func (o *Opener) Open(sealed []byte) ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failed || o.counter == math.MaxUint64 {
		o.failed = true
		return nil, ErrIntegrity
	}
	plaintext, err := o.aead.Open(nil, nonceFor(o.counter), sealed, aad)
	if err != nil {
		o.failed = true
		return nil, ErrIntegrity
	}
	o.counter++
	return plaintext, nil
}

// SealBody seals an HTTPS answer body (counter 0 of the bridge → device key).
func SealBody(key, plaintext []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nonceFor(0), plaintext, aad), nil
}

// OpenBody opens a sealed HTTPS answer body.
func OpenBody(key, sealed []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonceFor(0), sealed, aad)
	if err != nil {
		return nil, ErrIntegrity
	}
	return plaintext, nil
}
