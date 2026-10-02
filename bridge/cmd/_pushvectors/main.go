// Generates docs/protocol/fixtures/crypto/push-vectors.json (ADR-0010).
// Fully deterministic: fixed device and ephemeral keys.
package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"

	"github.com/JoKeks2023/housephone/bridge/internal/pushseal"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// key derives a fixed X25519 private key from a label.
func key(label string) *ecdh.PrivateKey {
	seed := sha256.Sum256([]byte("housephone push vectors " + label))
	return must(ecdh.X25519().NewPrivateKey(seed[:]))
}

func main() {
	plain := must(os.ReadFile("../docs/protocol/fixtures/push.incoming_call.json"))
	plain = bytes.TrimSpace(plain)
	device, eph := key("device"), key("ephemeral")
	sealed := must(pushseal.SealWithEphemeral(eph, device.PublicKey().Bytes(), plain))
	body := must(json.Marshal(map[string]string{"sealed": b64(sealed)}))
	out := map[string]any{
		"info":                pushseal.Info,
		"devicePrivateKey":    b64(device.Bytes()),
		"devicePublicKey":     b64(device.PublicKey().Bytes()),
		"ephemeralPrivateKey": b64(eph.Bytes()),
		"ephemeralPublicKey":  b64(eph.PublicKey().Bytes()),
		"plaintext":           string(plain),
		"sealed":              b64(sealed),
		"apnsBody":            string(body),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		panic(err)
	}
}
