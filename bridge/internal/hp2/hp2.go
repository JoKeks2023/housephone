// Package hp2 implements pairing, request authentication and end-to-end
// sealing of signaling protocol v2 (ADR-0004):
//
//   - devices sign requests with a P-256 key (Secure Enclave on iPhone and
//     Apple Watch), the bridge stores only the public key;
//   - the bridge signs every answer with its Ed25519 identity, whose
//     fingerprint the device pins from the pairing QR code;
//   - both sides agree on per-request keys (X25519 + HKDF-SHA256) and seal
//     WebSocket frames and HTTPS answers with ChaCha20-Poly1305.
//
// Only standard primitives from the Go standard library and x/crypto are
// used. The canonical signature inputs below must match the apps byte for
// byte; docs/protocol/fixtures/crypto/hp2-vectors.json pins them.
package hp2

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Protocol constants.
const (
	// Scheme is the Authorization scheme of device requests.
	Scheme = "HP2"
	// BridgeHeader carries the bridge's signature on every answer.
	BridgeHeader = "HP2-Bridge"
	// SealedContentType marks a sealed HTTPS answer body.
	SealedContentType = "application/vnd.housephone.sealed"

	// MaxClockSkew is the accepted difference between the device clock
	// (ts) and the bridge clock.
	MaxClockSkew = 60 * time.Second
	// NonceWindow is how long a used nonce is remembered per device; it
	// covers MaxClockSkew in both directions.
	NonceWindow = 2 * MaxClockSkew

	// NonceSize, EphemeralKeySize and SignatureSize are the decoded sizes
	// of the header fields.
	NonceSize        = 16
	EphemeralKeySize = 32
	SignatureSize    = 64
	// DevicePublicKeySize is an uncompressed X9.63 P-256 point.
	DevicePublicKeySize = 65
	// BridgePublicKeySize is an Ed25519 public key.
	BridgePublicKeySize = 32

	// FrameJSON and FrameAudio are the type bytes inside a sealed
	// WebSocket frame.
	FrameJSON  byte = 0x00
	FrameAudio byte = 0x01
)

// aad is the associated data of every sealed frame and body.
var aad = []byte("HP2")

// ErrMalformed reports a header or field that does not follow the format.
var ErrMalformed = errors.New("hp2: malformed")

// B64 encodes base64url without padding.
func B64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// DecodeB64 decodes strict base64url without padding.
func DecodeB64(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, ErrMalformed
	}
	return b, nil
}

// decodeSized decodes base64url and requires exactly n bytes.
func decodeSized(s string, n int) ([]byte, error) {
	b, err := DecodeB64(s)
	if err != nil || len(b) != n {
		return nil, ErrMalformed
	}
	return b, nil
}

// BodyHash is the lower-case hex SHA-256 of a body (SHA-256 of "" for none).
func BodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// NormalizeCode returns a pairing code in its canonical form: upper case,
// without the grouping hyphens and spaces people may type. Signatures always
// cover the canonical form.
func NormalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', ' ', '\t':
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(code)))
}

// GroupCode formats a canonical code as XXXX-XXXX-XXXX-XXXX for display.
func GroupCode(code string) string {
	var groups []string
	for i := 0; i < len(code); i += 4 {
		end := min(i+4, len(code))
		groups = append(groups, code[i:end])
	}
	return strings.Join(groups, "-")
}

func join(parts ...string) []byte { return []byte(strings.Join(parts, "\n")) }

// PairProofMessage is what the device signs when pairing to prove it owns
// its key. code is canonical (NormalizeCode).
func PairProofMessage(code, nonce, publicKey string) []byte {
	return join("HP2-PAIR-PROOF", code, nonce, publicKey)
}

// PairResponseMessage is what the bridge signs in the pairing answer. code
// is canonical (NormalizeCode).
func PairResponseMessage(bridgeID, deviceID, publicKey, nonce, code string) []byte {
	return join("HP2-PAIR", bridgeID, deviceID, publicKey, nonce, code)
}

// AuthInput are the fields of a signed device request.
type AuthInput struct {
	Method     string
	RequestURI string
	BridgeID   string
	DeviceID   string
	TS         int64
	Nonce      string
	EPK        string
	Body       []byte
}

// AuthMessage is what the device signs for every request.
func AuthMessage(in AuthInput) []byte {
	return join("HP2-AUTH", strings.ToUpper(in.Method), in.RequestURI, in.BridgeID, in.DeviceID,
		strconv.FormatInt(in.TS, 10), in.Nonce, in.EPK, BodyHash(in.Body))
}

// BridgeInput are the fields of a signed bridge answer.
type BridgeInput struct {
	BridgeID  string
	DeviceID  string
	Nonce     string
	DeviceEPK string
	BridgeEPK string
	Status    int
	// Body is the body exactly as sent (sealed if sealed); nil for 101,
	// 204 and 304.
	Body []byte
}

// BridgeMessage is what the bridge signs for every answer.
func BridgeMessage(in BridgeInput) []byte {
	return join("HP2-BRIDGE", in.BridgeID, in.DeviceID, in.Nonce, in.DeviceEPK, in.BridgeEPK,
		strconv.Itoa(in.Status), BodyHash(in.Body))
}

// KeysInfo is the HKDF info for the session keys of one request.
func KeysInfo(bridgeID, deviceID, deviceEPK, bridgeEPK string) string {
	return string(join("HP2-KEYS", bridgeID, deviceID, deviceEPK, bridgeEPK))
}
