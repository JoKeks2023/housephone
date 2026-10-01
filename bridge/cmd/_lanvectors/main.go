// Generates docs/protocol/fixtures/crypto/lan-pairing-vectors.json
// (ADR-0007): pairing in the home network with a confirmation code.
// Written against the primitives directly, not package hp2, so the vectors
// check the implementation instead of repeating it. Deterministic except
// the ECDSA proof signature (verify-only).
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
func lines(parts ...string) []byte {
	return []byte(strings.Join(parts, "\n"))
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func main() {
	// Long-term keys as in hp2-vectors.json.
	d := new(big.Int).SetBytes(must(hex.DecodeString("c9afa9d845ba75166b5c215767b1d6934e50c3db36e89b127b8a622b120f6721")))
	devPriv := &ecdsa.PrivateKey{D: d}
	devPriv.PublicKey.Curve = elliptic.P256()
	devPriv.PublicKey.X, devPriv.PublicKey.Y = elliptic.P256().ScalarBaseMult(d.Bytes())
	devPub := b64(elliptic.Marshal(elliptic.P256(), devPriv.PublicKey.X, devPriv.PublicKey.Y)) //nolint:staticcheck // X9.63 uncompressed
	bridgePriv := ed25519.NewKeyFromSeed(must(hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")))
	bridgePub := b64(bridgePriv.Public().(ed25519.PublicKey))

	bridgeID := "e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e"
	deviceID := "9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	publicURL := "wss://housephone.example.org/v1/ws"
	lanURL := "ws://192.168.178.20:8081/v1/ws"
	pairingID := b64(must(hex.DecodeString("a0a1a2a3a4a5a6a7a8a9aaabacadaeaf")))

	x := ecdh.X25519()
	devEph := must(x.NewPrivateKey(must(hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a"))))
	brEph := must(x.NewPrivateKey(must(hex.DecodeString("5dab087e624a8a4b79e17f8b83800ee66f3bb1292618b6fd1c2f8b27ff88e0eb"))))
	devEphPub := b64(devEph.PublicKey().Bytes())
	brEphPub := b64(brEph.PublicKey().Bytes())
	devNonce := b64(must(hex.DecodeString("101112131415161718191a1b1c1d1e1f")))
	brNonce := b64(must(hex.DecodeString("202122232425262728292a2b2c2d2e2f")))

	commitMsg := lines("HP2-LAN-COMMIT", devPub, devEphPub, devNonce)
	commitment := b64(sha(commitMsg))
	offerMsg := lines("HP2-LAN-OFFER", pairingID, bridgeID, devPub, commitment, bridgePub, brEphPub, brNonce)
	offerSig := ed25519.Sign(bridgePriv, offerMsg)
	transcriptMsg := lines("HP2-LAN-TRANSCRIPT", pairingID, bridgeID, devPub, commitment, bridgePub, brEphPub, brNonce, devEphPub, devNonce)
	th := sha(transcriptMsg)
	proofMsg := lines("HP2-LAN-PROOF", b64(th))
	r, s := must2(ecdsa.Sign(rand.Reader, devPriv, sha(proofMsg)))
	proof := make([]byte, 64)
	r.FillBytes(proof[:32])
	s.FillBytes(proof[32:])

	shared := must(devEph.ECDH(brEph.PublicKey()))
	sasBytes := must(hkdf.Key(sha256.New, shared, th, "HP2-LAN-SAS", 4))
	sas := fmt.Sprintf("%06d", binary.BigEndian.Uint32(sasBytes)%1_000_000)
	sealKey := must(hkdf.Key(sha256.New, shared, th, "HP2-LAN-SEAL", 32))

	approvedMsg := lines("HP2-LAN-APPROVED", b64(th), deviceID, bridgeID, publicURL, lanURL)
	approvedSig := b64(ed25519.Sign(bridgePriv, approvedMsg))
	approval := must(json.Marshal(map[string]string{
		"deviceId": deviceID, "bridgeId": bridgeID, "bridgeName": "Zuhause",
		"publicUrl": publicURL, "lanUrl": lanURL, "signature": approvedSig,
	}))
	aead := must(chacha20poly1305.New(sealKey))
	sealed := aead.Seal(nil, make([]byte, 12), approval, []byte("HP2"))

	out := map[string]any{
		"description": "HP2 pairing in the home network (ADR-0007). Long-term keys as in hp2-vectors.json. base64url without padding, hex lowercase. The ECDSA proof is random: verify it, do not reproduce it. Everything else is deterministic.",
		"ids":         map[string]string{"bridgeId": bridgeID, "deviceId": deviceID, "pairingId": pairingID},
		"urls":        map[string]string{"publicUrl": publicURL, "lanUrl": lanURL},
		"keys": map[string]string{
			"devicePublicKeyX963":             devPub,
			"bridgePublicKey":                 bridgePub,
			"deviceEphemeralPrivateX25519Hex": hx(devEph.Bytes()),
			"deviceEphemeralPublic":           devEphPub,
			"bridgeEphemeralPrivateX25519Hex": hx(brEph.Bytes()),
			"bridgeEphemeralPublic":           brEphPub,
			"deviceNonce":                     devNonce,
			"bridgeNonce":                     brNonce,
		},
		"steps": map[string]string{
			"commitMessage":     string(commitMsg),
			"commitment":        commitment,
			"offerMessage":      string(offerMsg),
			"offerSignature":    b64(offerSig),
			"transcriptMessage": string(transcriptMsg),
			"transcriptHashHex": hx(th),
			"proofMessage":      string(proofMsg),
			"proofSignature":    b64(proof),
			"sharedSecretHex":   hx(shared),
			"sasOkmHex":         hx(sasBytes),
			"sas":               sas,
			"sealKeyHex":        hx(sealKey),
			"approvedMessage":   string(approvedMsg),
			"approvedSignature": approvedSig,
			"approvalPlaintext": string(approval),
			"approvalSealedHex": hx(sealed),
			"approvalSealed":    b64(sealed),
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		panic(err)
	}
}

func must2(r, s *big.Int, err error) (*big.Int, *big.Int) {
	if err != nil {
		panic(err)
	}
	return r, s
}
