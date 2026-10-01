// Generates docs/protocol/fixtures/crypto/admin-vectors.json (ADR-0009):
// the admin key's signatures over an admin request and over its
// enrollment. Written against the primitives directly, not package hp2, so
// the vectors check the implementation instead of repeating it. The ECDSA
// signatures are random: verify them, do not reproduce them.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func sha(b []byte) []byte { s := sha256.Sum256(b); return s[:] }
func lines(parts ...string) []byte {
	return []byte(strings.Join(parts, "\n"))
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func key(scalarHex string) (*ecdsa.PrivateKey, string) {
	d := new(big.Int).SetBytes(must(hex.DecodeString(scalarHex)))
	priv := &ecdsa.PrivateKey{D: d}
	priv.PublicKey.Curve = elliptic.P256()
	priv.PublicKey.X, priv.PublicKey.Y = elliptic.P256().ScalarBaseMult(d.Bytes())
	return priv, b64(elliptic.Marshal(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y)) //nolint:staticcheck // X9.63 uncompressed
}

func sign(priv *ecdsa.PrivateKey, msg []byte) string {
	r, s, err := ecdsa.Sign(rand.Reader, priv, sha(msg))
	if err != nil {
		panic(err)
	}
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return b64(out)
}

func main() {
	// Device key and request as in hp2-vectors.json; the admin key is a
	// second, unrelated P-256 key.
	_, devPub := key("c9afa9d845ba75166b5c215767b1d6934e50c3db36e89b127b8a622b120f6721")
	adminPriv, adminPub := key("0f5e4d3c2b1a09f8e7d6c5b4a3928170f1e2d3c4b5a69788796a5b4c3d2e1f00")

	bridgeID := "e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e"
	deviceID := "9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	method, path := "PUT", "/v1/admin/devices/3c4d5e6f-7081-4293-a4b5-c6d7e8f90a1b"
	body := `{"name":"Küche"}`
	ts, nonce, epk := "1790000000", "8OHSw7Sllod4aVpLPC0eDw", "hSDwCYkwp1R0i33ctD73Wg2_Og0mOBr066SpjqqbTmo"
	bodyHash := hex.EncodeToString(sha([]byte(body)))

	adminMsg := lines("HP2-ADMIN", method, path, bridgeID, deviceID, ts, nonce, epk, bodyHash)
	enrollMsg := lines("HP2-ADMIN-ENROLL", bridgeID, deviceID, adminPub)

	out := map[string]any{
		"description": "HP2 administration from the app (ADR-0009). Device key and request fields as in hp2-vectors.json. base64url without padding, hex lowercase. The ECDSA signatures are random: verify them, do not reproduce them.",
		"ids":         map[string]string{"bridgeId": bridgeID, "deviceId": deviceID},
		"keys": map[string]string{
			"devicePublicKeyX963":   devPub,
			"adminPrivateScalarHex": hex.EncodeToString(adminPriv.D.FillBytes(make([]byte, 32))),
			"adminPublicKeyX963":    adminPub,
		},
		"request": map[string]string{
			"method": method, "path": path, "body": body, "bodySha256Hex": bodyHash,
			"ts": ts, "nonce": nonce, "epk": epk,
			"adminMessage":   string(adminMsg),
			"adminSignature": sign(adminPriv, adminMsg),
		},
		"enroll": map[string]string{
			"enrollMessage": string(enrollMsg),
			"proof":         sign(adminPriv, enrollMsg),
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		panic(err)
	}
}
