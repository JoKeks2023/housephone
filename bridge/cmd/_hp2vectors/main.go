// Generates docs/protocol/fixtures/crypto/hp2-vectors.json (ADR-0004).
// Deterministic except the ECDSA sample signatures (verify-only).
package main

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func hx(b []byte) string  { return hex.EncodeToString(b) }
func sha(b []byte) []byte { s := sha256.Sum256(b); return s[:] }
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func ecdsaSign(priv *ecdsa.PrivateKey, msg []byte) []byte {
	r, s := must2(ecdsa.Sign(rand.Reader, priv, sha(msg)))
	out := make([]byte, 64)
	r.FillBytes(out[:32])
	s.FillBytes(out[32:])
	return out
}
func must2(r, s *big.Int, err error) (*big.Int, *big.Int) {
	if err != nil {
		panic(err)
	}
	return r, s
}

func seal(key []byte, counter uint64, plaintext []byte) []byte {
	aead := must(chacha20poly1305.New(key))
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], counter)
	return aead.Seal(nil, nonce, plaintext, []byte("HP2"))
}

func lines(parts ...string) string { return strings.Join(parts, "\n") }

func main() {
	// Fixed long-term keys.
	d := new(big.Int).SetBytes(must(hex.DecodeString("c9afa9d845ba75166b5c215767b1d6934e50c3db36e89b127b8a622b120f6721")))
	devPriv := &ecdsa.PrivateKey{D: d}
	devPriv.PublicKey.Curve = elliptic.P256()
	devPriv.PublicKey.X, devPriv.PublicKey.Y = elliptic.P256().ScalarBaseMult(d.Bytes())
	devPub := elliptic.Marshal(elliptic.P256(), devPriv.PublicKey.X, devPriv.PublicKey.Y) //nolint:staticcheck // X9.63 uncompressed
	bridgeSeed := must(hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"))
	bridgePriv := ed25519.NewKeyFromSeed(bridgeSeed)
	bridgePub := bridgePriv.Public().(ed25519.PublicKey)

	bridgeID := "e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e"
	deviceID := "9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d"

	// Pairing.
	code := "K7P2XH9QRMW4DZT8"
	pairNonce := must(hex.DecodeString("000102030405060708090a0b0c0d0e0f"))
	proofMsg := lines("HP2-PAIR-PROOF", code, b64(pairNonce), b64(devPub))
	pairMsg := lines("HP2-PAIR", bridgeID, deviceID, b64(devPub), b64(pairNonce), code)

	// Request auth.
	x := ecdh.X25519()
	devEph := must(x.NewPrivateKey(must(hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a"))))
	brEph := must(x.NewPrivateKey(must(hex.DecodeString("5dab087e624a8a4b79e17f8b83800ee66f3bb1292618b6fd1c2f8b27ff88e0eb"))))
	reqNonce := must(hex.DecodeString("f0e1d2c3b4a5968778695a4b3c2d1e0f"))
	ts := "1790000000"
	body := []byte(`{"pushToken":"a1b2","pushEnvironment":"production"}`)
	authMsg := lines("HP2-AUTH", "PUT", "/v1/device", bridgeID, deviceID, ts, b64(reqNonce), b64(devEph.PublicKey().Bytes()), hx(sha(body)))
	authSig := ecdsaSign(devPriv, []byte(authMsg))
	header := fmt.Sprintf("HP2 id=%s, ts=%s, nonce=%s, epk=%s, sig=%s", deviceID, ts, b64(reqNonce), b64(devEph.PublicKey().Bytes()), b64(authSig))

	// Keys.
	shared := must(devEph.ECDH(brEph.PublicKey()))
	info := lines("HP2-KEYS", bridgeID, deviceID, b64(devEph.PublicKey().Bytes()), b64(brEph.PublicKey().Bytes()))
	okm := must(hkdf.Key(sha256.New, shared, reqNonce, info, 64))
	kD2B, kB2D := okm[:32], okm[32:]

	// Sealed HTTPS response + bridge signature.
	respJSON := []byte(`{"status":"ok"}`)
	sealedResp := seal(kB2D, 0, respJSON)
	bridgeMsg := lines("HP2-BRIDGE", bridgeID, deviceID, b64(reqNonce), b64(devEph.PublicKey().Bytes()), b64(brEph.PublicKey().Bytes()), "200", hx(sha(sealedResp)))
	bridgeMsg101 := lines("HP2-BRIDGE", bridgeID, deviceID, b64(reqNonce), b64(devEph.PublicKey().Bytes()), b64(brEph.PublicKey().Bytes()), "101", hx(sha(nil)))

	// WebSocket frames.
	helloPT := append([]byte{0x00}, []byte(`{"type":"hello","payload":{"appVersion":"1.0 (1)","platform":"ios"}}`)...)
	welcomePT := append([]byte{0x00}, []byte(`{"type":"welcome","payload":{"bridgeId":"e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e","bridgeName":"Zuhause","bridgeVersion":"0.4.0","sipRegistered":true}}`)...)
	audio := make([]byte, 160)
	for i := range audio {
		audio[i] = 0xD5
	}
	audioPT := append([]byte{0x01}, audio...)

	out := map[string]any{
		"description": "HP2 test vectors (ADR-0004). base64url without padding, hex lowercase. ECDSA signatures are random: implementations must VERIFY the given ones and may not expect to reproduce them. Everything else is deterministic.",
		"ids":         map[string]string{"bridgeId": bridgeID, "deviceId": deviceID},
		"keys": map[string]string{
			"devicePrivateKeyScalarHex":      hx(d.FillBytes(make([]byte, 32))),
			"devicePublicKeyX963":            b64(devPub),
			"bridgeEd25519SeedHex":           hx(bridgeSeed),
			"bridgePublicKey":                b64(bridgePub),
			"bridgeFingerprint":              b64(sha(bridgePub)),
			"deviceEphemeralPrivateX25519Hex": hx(devEph.Bytes()),
			"deviceEphemeralPublic":          b64(devEph.PublicKey().Bytes()),
			"bridgeEphemeralPrivateX25519Hex": hx(brEph.Bytes()),
			"bridgeEphemeralPublic":          b64(brEph.PublicKey().Bytes()),
		},
		"pairing": map[string]string{
			"code":             code,
			"nonce":            b64(pairNonce),
			"proofMessage":     proofMsg,
			"proofSignature":   b64(ecdsaSign(devPriv, []byte(proofMsg))),
			"responseMessage":  pairMsg,
			"responseSignature": b64(ed25519.Sign(bridgePriv, []byte(pairMsg))),
			"link":             "housephone://pair?v=2&url=wss%3A%2F%2Fphone.example.com%2Fv1%2Fws&code=" + code + "&fp=" + b64(sha(bridgePub)) + "&name=Zuhause",
		},
		"request": map[string]string{
			"method":              "PUT",
			"path":                "/v1/device",
			"ts":                  ts,
			"nonce":               b64(reqNonce),
			"body":                string(body),
			"bodySha256Hex":       hx(sha(body)),
			"signedMessage":       authMsg,
			"signature":           b64(authSig),
			"authorizationHeader": header,
		},
		"keyDerivation": map[string]string{
			"sharedSecretHex": hx(shared),
			"hkdfSaltHex":     hx(reqNonce),
			"hkdfInfo":        info,
			"okmHex":          hx(okm),
			"deviceToBridgeKeyHex": hx(kD2B),
			"bridgeToDeviceKeyHex": hx(kB2D),
		},
		"response": map[string]string{
			"status":            "200",
			"plaintextBody":     string(respJSON),
			"sealedBodyHex":     hx(sealedResp),
			"signedMessage":     bridgeMsg,
			"signature":         b64(ed25519.Sign(bridgePriv, []byte(bridgeMsg))),
			"bridgeHeader":      "epk=" + b64(brEph.PublicKey().Bytes()) + ", sig=" + b64(ed25519.Sign(bridgePriv, []byte(bridgeMsg))),
			"upgrade101SignedMessage": bridgeMsg101,
			"upgrade101Signature":     b64(ed25519.Sign(bridgePriv, []byte(bridgeMsg101))),
		},
		"frames": []map[string]string{
			{"direction": "deviceToBridge", "counter": "0", "plaintextHex": hx(helloPT), "sealedHex": hx(seal(kD2B, 0, helloPT))},
			{"direction": "bridgeToDevice", "counter": "0", "plaintextHex": hx(welcomePT), "sealedHex": hx(seal(kB2D, 0, welcomePT))},
			{"direction": "deviceToBridge", "counter": "1", "plaintextHex": hx(audioPT), "sealedHex": hx(seal(kD2B, 1, audioPT))},
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		panic(err)
	}
}
