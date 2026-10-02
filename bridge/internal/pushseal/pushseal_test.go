package pushseal

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"testing"
)

func TestSealOpen(t *testing.T) {
	dev, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(`{"type":"incoming_call"}`)
	sealed, err := Seal(rand.Reader, dev.PublicKey().Bytes(), msg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(dev, sealed)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("Open = %q, %v", got, err)
	}

	other, _ := ecdh.X25519().GenerateKey(rand.Reader)
	if _, err := Open(other, sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("wrong key: %v", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := Open(dev, sealed); !errors.Is(err, ErrOpen) {
		t.Fatalf("tampered: %v", err)
	}
}

func TestSealRejects(t *testing.T) {
	if _, err := Seal(rand.Reader, make([]byte, 31), nil); !errors.Is(err, ErrBadKey) {
		t.Fatalf("short key: %v", err)
	}
	// All-zero is a low-order point.
	if _, err := Seal(rand.Reader, make([]byte, 32), []byte("x")); !errors.Is(err, ErrBadKey) {
		t.Fatalf("zero key: %v", err)
	}
	dev, _ := ecdh.X25519().GenerateKey(rand.Reader)
	if _, err := Seal(rand.Reader, dev.PublicKey().Bytes(), make([]byte, MaxPlaintext+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large: %v", err)
	}
	if ValidKey(make([]byte, 32)) || ValidKey(make([]byte, 31)) || !ValidKey(dev.PublicKey().Bytes()) {
		t.Fatal("ValidKey")
	}
}
