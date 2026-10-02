package pushseal

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

// The shared vectors (docs/protocol/fixtures/crypto/push-vectors.json) are
// what the app tests against too.
func TestVectors(t *testing.T) {
	data, err := os.ReadFile("../../../docs/protocol/fixtures/crypto/push-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	dec := func(name string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(v[name])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return b
	}
	device, err := ecdh.X25519().NewPrivateKey(dec("devicePrivateKey"))
	if err != nil {
		t.Fatal(err)
	}
	eph, err := ecdh.X25519().NewPrivateKey(dec("ephemeralPrivateKey"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealWithEphemeral(eph, dec("devicePublicKey"), []byte(v["plaintext"]))
	if err != nil {
		t.Fatal(err)
	}
	if base64.RawURLEncoding.EncodeToString(sealed) != v["sealed"] {
		t.Fatal("sealed differs from the vector")
	}
	plain, err := Open(device, dec("sealed"))
	if err != nil || string(plain) != v["plaintext"] {
		t.Fatalf("Open = %q, %v", plain, err)
	}
}
