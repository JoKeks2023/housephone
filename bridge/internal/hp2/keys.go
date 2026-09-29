package hp2

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ParseDevicePublicKey decodes a device key: base64url of an uncompressed
// X9.63 P-256 point (65 bytes). Points not on the curve are rejected.
func ParseDevicePublicKey(b64 string) (*ecdsa.PublicKey, error) {
	raw, err := decodeSized(b64, DevicePublicKeySize)
	if err != nil {
		return nil, err
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		return nil, fmt.Errorf("%w: device public key: %v", ErrMalformed, err)
	}
	return pub, nil
}

// VerifyDeviceSignature checks a raw r‖s ECDSA P-256 signature over
// SHA-256(msg), as CryptoKit's P256.Signing produces it.
func VerifyDeviceSignature(pub *ecdsa.PublicKey, msg, sig []byte) bool {
	if pub == nil || len(sig) != SignatureSize {
		return false
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	digest := sha256.Sum256(msg)
	return ecdsa.Verify(pub, digest[:], r, s)
}

// VerifyBridgeSignature checks an Ed25519 signature of the bridge.
func VerifyBridgeSignature(pub ed25519.PublicKey, msg, sig []byte) bool {
	return len(pub) == ed25519.PublicKeySize && len(sig) == ed25519.SignatureSize && ed25519.Verify(pub, msg, sig)
}

// Identity is the bridge's long-term Ed25519 signing key.
type Identity struct {
	priv ed25519.PrivateKey
}

// NewIdentity creates the identity from its 32-byte seed.
func NewIdentity(seed []byte) (*Identity, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("hp2: identity seed must be 32 bytes")
	}
	return &Identity{priv: ed25519.NewKeyFromSeed(seed)}, nil
}

// NewIdentitySeed returns a fresh random seed.
func NewIdentitySeed() ([]byte, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	return seed, nil
}

// PublicKey returns the public key.
func (i *Identity) PublicKey() ed25519.PublicKey { return i.priv.Public().(ed25519.PublicKey) }

// Sign signs msg.
func (i *Identity) Sign(msg []byte) []byte { return ed25519.Sign(i.priv, msg) }

// Fingerprint is the value of fp= in the pairing link.
func (i *Identity) Fingerprint() string { return Fingerprint(i.PublicKey()) }

// Fingerprint is base64url(SHA-256(pub)).
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return B64(sum[:])
}

// FingerprintHex is the fingerprint as upper-case hex in groups of four,
// for reading it out and comparing by eye.
func FingerprintHex(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	groups := make([]string, 0, len(h)/4)
	for i := 0; i < len(h); i += 4 {
		groups = append(groups, h[i:i+4])
	}
	return strings.Join(groups, " ")
}

// KeyFingerprint is a short hex fingerprint of a device public key
// (base64url X9.63) for logs and devices list.
func KeyFingerprint(publicKeyB64 string) string {
	raw, err := DecodeB64(publicKeyB64)
	if err != nil || len(raw) == 0 {
		return "-"
	}
	sum := sha256.Sum256(raw)
	return strings.ToUpper(hex.EncodeToString(sum[:6]))
}

// NewEphemeral returns a fresh X25519 key for one request.
func NewEphemeral() (*ecdh.PrivateKey, error) { return ecdh.X25519().GenerateKey(rand.Reader) }

// DeviceKey signs requests. On the Apple devices it lives in the Secure
// Enclave; the probe and tests use SoftwareKey.
type DeviceKey interface {
	// PublicKeyX963 returns the uncompressed P-256 public key.
	PublicKeyX963() []byte
	// Sign returns a raw r‖s ECDSA signature over SHA-256(msg).
	Sign(msg []byte) ([]byte, error)
}

// SoftwareKey is a P-256 key held in memory (probe, tests).
type SoftwareKey struct {
	priv *ecdsa.PrivateKey
}

// NewSoftwareKey generates a key.
func NewSoftwareKey() (*SoftwareKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &SoftwareKey{priv: priv}, nil
}

// SoftwareKeyFromScalar restores a key from its 32-byte private scalar.
func SoftwareKeyFromScalar(scalar []byte) (*SoftwareKey, error) {
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	if err != nil {
		return nil, err
	}
	return &SoftwareKey{priv: priv}, nil
}

// Scalar returns the 32-byte private scalar (for storing the key).
func (k *SoftwareKey) Scalar() ([]byte, error) { return k.priv.Bytes() }

// PublicKeyX963 implements DeviceKey.
func (k *SoftwareKey) PublicKeyX963() []byte {
	raw, _ := k.priv.PublicKey.Bytes()
	return raw
}

// Sign implements DeviceKey.
func (k *SoftwareKey) Sign(msg []byte) ([]byte, error) {
	digest := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, k.priv, digest[:])
	if err != nil {
		return nil, err
	}
	out := make([]byte, SignatureSize)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return out, nil
}
