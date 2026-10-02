// Package pushseal encrypts VoIP push payloads end to end for one device
// (ADR-0010): neither the push relay nor Apple can read the caller.
//
// The device holds an X25519 key pair (the push key) and tells the bridge
// the public half. For every push the bridge creates an ephemeral X25519 key
// and seals the payload:
//
//	shared = X25519(ephemeral, device push key)
//	key    = HKDF-SHA256(ikm = shared, salt = ephemeral pub || device pub,
//	                     info = "housephone-push-v1", 32 bytes)
//	box    = ChaCha20-Poly1305(key, nonce = 12 zero bytes,
//	                           aad = "housephone-push-v1", plaintext)
//	sealed = ephemeral pub (32 bytes) || box
//
// Every key is used once, so the fixed nonce is safe.
package pushseal

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	crand "crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// Info is the HKDF info and the AEAD associated data.
const Info = "housephone-push-v1"

// KeySize is the length of a raw X25519 public key.
const KeySize = 32

// MaxPlaintext keeps the sealed payload, base64url encoded, well below the
// 5 KB APNs limit for VoIP pushes.
const MaxPlaintext = 2048

var (
	// ErrBadKey reports a push key that is not a usable X25519 public key.
	ErrBadKey = errors.New("pushseal: invalid push key")
	// ErrOpen reports a sealed payload that fails authentication.
	ErrOpen = errors.New("pushseal: cannot open payload")
	// ErrTooLarge reports a plaintext above MaxPlaintext.
	ErrTooLarge = errors.New("pushseal: payload too large")
)

// Seal encrypts plaintext for the device's public push key.
func Seal(rand io.Reader, devicePublic, plaintext []byte) ([]byte, error) {
	eph, err := ecdh.X25519().GenerateKey(rand)
	if err != nil {
		return nil, err
	}
	return SealWithEphemeral(eph, devicePublic, plaintext)
}

// SealWithEphemeral is Seal with a given ephemeral key. Only for test
// vectors: reusing an ephemeral key breaks the fixed-nonce construction.
func SealWithEphemeral(eph *ecdh.PrivateKey, devicePublic, plaintext []byte) ([]byte, error) {
	if len(plaintext) > MaxPlaintext {
		return nil, ErrTooLarge
	}
	peer, err := ecdh.X25519().NewPublicKey(devicePublic)
	if err != nil {
		return nil, ErrBadKey
	}
	shared, err := eph.ECDH(peer)
	if err != nil {
		// Low-order point.
		return nil, ErrBadKey
	}
	aead, err := newAEAD(shared, eph.PublicKey().Bytes(), devicePublic)
	if err != nil {
		return nil, err
	}
	out := append([]byte{}, eph.PublicKey().Bytes()...)
	return aead.Seal(out, make([]byte, chacha20poly1305.NonceSize), plaintext, []byte(Info)), nil
}

// Open decrypts a sealed payload with the device's private push key. The
// app does the same; the bridge only needs it in tests.
func Open(device *ecdh.PrivateKey, sealed []byte) ([]byte, error) {
	if len(sealed) < KeySize+chacha20poly1305.Overhead {
		return nil, ErrOpen
	}
	eph, err := ecdh.X25519().NewPublicKey(sealed[:KeySize])
	if err != nil {
		return nil, ErrOpen
	}
	shared, err := device.ECDH(eph)
	if err != nil {
		return nil, ErrOpen
	}
	aead, err := newAEAD(shared, sealed[:KeySize], device.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, make([]byte, chacha20poly1305.NonceSize), sealed[KeySize:], []byte(Info))
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}

// ValidKey reports whether raw is a usable X25519 public key: 32 bytes and
// not a low-order point (which would make every shared secret zero).
func ValidKey(raw []byte) bool {
	pub, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return false
	}
	probe, err := ecdh.X25519().GenerateKey(crand.Reader)
	if err != nil {
		return false
	}
	_, err = probe.ECDH(pub)
	return err == nil
}

func newAEAD(shared, ephemeralPublic, devicePublic []byte) (cipher.AEAD, error) {
	salt := append(append([]byte{}, ephemeralPublic...), devicePublic...)
	key, err := hkdf.Key(sha256.New, shared, salt, Info, chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	return chacha20poly1305.New(key)
}
