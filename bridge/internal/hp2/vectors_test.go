package hp2

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// vectors mirrors docs/protocol/fixtures/crypto/hp2-vectors.json.
type vectors struct {
	IDs  struct{ BridgeID, DeviceID string } `json:"ids"`
	Keys struct {
		DevicePrivateKeyScalarHex       string `json:"devicePrivateKeyScalarHex"`
		DevicePublicKeyX963             string `json:"devicePublicKeyX963"`
		BridgeEd25519SeedHex            string `json:"bridgeEd25519SeedHex"`
		BridgePublicKey                 string `json:"bridgePublicKey"`
		BridgeFingerprint               string `json:"bridgeFingerprint"`
		DeviceEphemeralPrivateX25519Hex string `json:"deviceEphemeralPrivateX25519Hex"`
		DeviceEphemeralPublic           string `json:"deviceEphemeralPublic"`
		BridgeEphemeralPrivateX25519Hex string `json:"bridgeEphemeralPrivateX25519Hex"`
		BridgeEphemeralPublic           string `json:"bridgeEphemeralPublic"`
	} `json:"keys"`
	Pairing struct {
		Code, Nonce, ProofMessage, ProofSignature, ResponseMessage, ResponseSignature, Link string
	} `json:"pairing"`
	Request struct {
		Method, Path, TS, Nonce, Body, BodySha256Hex, SignedMessage, Signature, AuthorizationHeader string
	} `json:"request"`
	KeyDerivation struct {
		SharedSecretHex, HkdfSaltHex, HkdfInfo, OkmHex, DeviceToBridgeKeyHex, BridgeToDeviceKeyHex string
	} `json:"keyDerivation"`
	Response struct {
		Status, PlaintextBody, SealedBodyHex, SignedMessage, Signature, BridgeHeader string
		Upgrade101SignedMessage                                                      string `json:"upgrade101SignedMessage"`
		Upgrade101Signature                                                          string `json:"upgrade101Signature"`
	} `json:"response"`
	Frames []struct {
		Direction, Counter, PlaintextHex, SealedHex string
	} `json:"frames"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	data, err := os.ReadFile("../../../docs/protocol/fixtures/crypto/hp2-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := DecodeB64(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return b
}

func eqString(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s:\n got %q\nwant %q", what, got, want)
	}
}

func TestVectorKeys(t *testing.T) {
	v := loadVectors(t)
	dev, err := SoftwareKeyFromScalar(mustHex(t, v.Keys.DevicePrivateKeyScalarHex))
	if err != nil {
		t.Fatal(err)
	}
	eqString(t, "device public key", B64(dev.PublicKeyX963()), v.Keys.DevicePublicKeyX963)
	if _, err := ParseDevicePublicKey(v.Keys.DevicePublicKeyX963); err != nil {
		t.Fatal(err)
	}

	id, err := NewIdentity(mustHex(t, v.Keys.BridgeEd25519SeedHex))
	if err != nil {
		t.Fatal(err)
	}
	eqString(t, "bridge public key", B64(id.PublicKey()), v.Keys.BridgePublicKey)
	eqString(t, "bridge fingerprint", id.Fingerprint(), v.Keys.BridgeFingerprint)

	for _, pair := range [][2]string{
		{v.Keys.DeviceEphemeralPrivateX25519Hex, v.Keys.DeviceEphemeralPublic},
		{v.Keys.BridgeEphemeralPrivateX25519Hex, v.Keys.BridgeEphemeralPublic},
	} {
		priv, err := ecdh.X25519().NewPrivateKey(mustHex(t, pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		eqString(t, "ephemeral public", B64(priv.PublicKey().Bytes()), pair[1])
	}
}

func TestVectorPairing(t *testing.T) {
	v := loadVectors(t)
	p := v.Pairing
	eqString(t, "proof message", string(PairProofMessage(p.Code, p.Nonce, v.Keys.DevicePublicKeyX963)), p.ProofMessage)
	pub, err := ParseDevicePublicKey(v.Keys.DevicePublicKeyX963)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyDeviceSignature(pub, []byte(p.ProofMessage), mustB64(t, p.ProofSignature)) {
		t.Fatal("proof signature from the vectors does not verify")
	}
	eqString(t, "response message", string(PairResponseMessage(v.IDs.BridgeID, v.IDs.DeviceID, v.Keys.DevicePublicKeyX963, p.Nonce, p.Code)), p.ResponseMessage)

	id, _ := NewIdentity(mustHex(t, v.Keys.BridgeEd25519SeedHex))
	eqString(t, "response signature (Ed25519 is deterministic)", B64(id.Sign([]byte(p.ResponseMessage))), p.ResponseSignature)

	req := protocol.PairRequest{Code: p.Code, PublicKey: v.Keys.DevicePublicKeyX963, Nonce: p.Nonce, Proof: p.ProofSignature}
	code, err := VerifyPairProof(req)
	if err != nil || code != p.Code {
		t.Fatalf("VerifyPairProof = %q, %v", code, err)
	}
	res := SignPairResponse(id, req, code, protocol.PairResponse{DeviceID: v.IDs.DeviceID, BridgeID: v.IDs.BridgeID, BridgeName: "Zuhause"})
	eqString(t, "signed pair response", res.Signature, p.ResponseSignature)
	pinned, err := VerifyPairResponse(req, res, v.Keys.BridgeFingerprint)
	if err != nil || !bytes.Equal(pinned, id.PublicKey()) {
		t.Fatalf("VerifyPairResponse: %v", err)
	}

	link, err := ParsePairingLink(p.Link)
	if err != nil {
		t.Fatal(err)
	}
	if link.Code != p.Code || link.Fingerprint != v.Keys.BridgeFingerprint || link.URL != "wss://phone.example.com/v1/ws" || link.Name != "Zuhause" {
		t.Fatalf("parsed link %+v", link)
	}
	eqString(t, "pairing link", FormatPairingLink(link.URL, link.Code, link.Fingerprint, link.Name), p.Link)
}

func TestVectorRequest(t *testing.T) {
	v := loadVectors(t)
	r := v.Request
	eqString(t, "body hash", BodyHash([]byte(r.Body)), r.BodySha256Hex)
	h, err := ParseAuthorization(r.AuthorizationHeader)
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := strconv.ParseInt(r.TS, 10, 64)
	if h.DeviceID != v.IDs.DeviceID || h.TS != ts || h.Nonce != r.Nonce || h.EPK != v.Keys.DeviceEphemeralPublic || B64(h.Sig) != r.Signature {
		t.Fatalf("parsed header %+v", h)
	}
	eqString(t, "header round trip", h.String(), r.AuthorizationHeader)
	msg := AuthMessage(AuthInput{Method: r.Method, RequestURI: r.Path, BridgeID: v.IDs.BridgeID, DeviceID: h.DeviceID, TS: h.TS, Nonce: h.Nonce, EPK: h.EPK, Body: []byte(r.Body)})
	eqString(t, "auth message", string(msg), r.SignedMessage)
	pub, _ := ParseDevicePublicKey(v.Keys.DevicePublicKeyX963)
	if !VerifyRequest(pub, h, r.Method, r.Path, v.IDs.BridgeID, []byte(r.Body)) {
		t.Fatal("request signature from the vectors does not verify")
	}
	// Any change to a signed field breaks the signature.
	for name, tamper := range map[string]func() bool{
		"method": func() bool { return VerifyRequest(pub, h, "POST", r.Path, v.IDs.BridgeID, []byte(r.Body)) },
		"path":   func() bool { return VerifyRequest(pub, h, r.Method, "/v1/device?x=1", v.IDs.BridgeID, []byte(r.Body)) },
		"bridge": func() bool { return VerifyRequest(pub, h, r.Method, r.Path, "other", []byte(r.Body)) },
		"body":   func() bool { return VerifyRequest(pub, h, r.Method, r.Path, v.IDs.BridgeID, []byte(r.Body+" ")) },
		"ts": func() bool {
			h2 := h
			h2.TS++
			return VerifyRequest(pub, h2, r.Method, r.Path, v.IDs.BridgeID, []byte(r.Body))
		},
	} {
		if tamper() {
			t.Errorf("signature still verifies with changed %s", name)
		}
	}
}

func TestVectorKeyDerivationAndSealing(t *testing.T) {
	v := loadVectors(t)
	k := v.KeyDerivation
	devEph, _ := ecdh.X25519().NewPrivateKey(mustHex(t, v.Keys.DeviceEphemeralPrivateX25519Hex))
	brEph, _ := ecdh.X25519().NewPrivateKey(mustHex(t, v.Keys.BridgeEphemeralPrivateX25519Hex))
	eqString(t, "hkdf info", KeysInfo(v.IDs.BridgeID, v.IDs.DeviceID, v.Keys.DeviceEphemeralPublic, v.Keys.BridgeEphemeralPublic), k.HkdfInfo)
	shared, err := devEph.ECDH(brEph.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	eqString(t, "shared secret", hex.EncodeToString(shared), k.SharedSecretHex)

	// Both roles derive the same keys.
	fromDevice, err := DeriveKeys(devEph, brEph.PublicKey().Bytes(), mustHex(t, k.HkdfSaltHex), k.HkdfInfo)
	if err != nil {
		t.Fatal(err)
	}
	fromBridge, err := DeriveKeys(brEph, devEph.PublicKey().Bytes(), mustHex(t, k.HkdfSaltHex), k.HkdfInfo)
	if err != nil {
		t.Fatal(err)
	}
	for _, keys := range []SessionKeys{fromDevice, fromBridge} {
		eqString(t, "device→bridge key", hex.EncodeToString(keys.DeviceToBridge), k.DeviceToBridgeKeyHex)
		eqString(t, "bridge→device key", hex.EncodeToString(keys.BridgeToDevice), k.BridgeToDeviceKeyHex)
	}
	eqString(t, "okm", hex.EncodeToString(append(append([]byte{}, fromDevice.DeviceToBridge...), fromDevice.BridgeToDevice...)), k.OkmHex)

	res := v.Response
	sealed, err := SealBody(fromBridge.BridgeToDevice, []byte(res.PlaintextBody))
	if err != nil {
		t.Fatal(err)
	}
	eqString(t, "sealed body", hex.EncodeToString(sealed), res.SealedBodyHex)
	opened, err := OpenBody(fromDevice.BridgeToDevice, sealed)
	if err != nil || string(opened) != res.PlaintextBody {
		t.Fatalf("OpenBody = %q, %v", opened, err)
	}
	sealed[0] ^= 1
	if _, err := OpenBody(fromDevice.BridgeToDevice, sealed); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("tampered body opened: %v", err)
	}

	id, _ := NewIdentity(mustHex(t, v.Keys.BridgeEd25519SeedHex))
	status, _ := strconv.Atoi(res.Status)
	msg := BridgeMessage(BridgeInput{BridgeID: v.IDs.BridgeID, DeviceID: v.IDs.DeviceID, Nonce: v.Request.Nonce,
		DeviceEPK: v.Keys.DeviceEphemeralPublic, BridgeEPK: v.Keys.BridgeEphemeralPublic, Status: status, Body: mustHex(t, res.SealedBodyHex)})
	eqString(t, "bridge message", string(msg), res.SignedMessage)
	eqString(t, "bridge signature", B64(id.Sign(msg)), res.Signature)
	eqString(t, "bridge header", FormatBridgeHeader(brEph.PublicKey().Bytes(), id.Sign(msg)), res.BridgeHeader)
	msg101 := BridgeMessage(BridgeInput{BridgeID: v.IDs.BridgeID, DeviceID: v.IDs.DeviceID, Nonce: v.Request.Nonce,
		DeviceEPK: v.Keys.DeviceEphemeralPublic, BridgeEPK: v.Keys.BridgeEphemeralPublic, Status: 101})
	eqString(t, "101 message", string(msg101), res.Upgrade101SignedMessage)
	eqString(t, "101 signature", B64(id.Sign(msg101)), res.Upgrade101Signature)
	bh, err := ParseBridgeHeader(res.BridgeHeader)
	if err != nil || !VerifyBridgeSignature(id.PublicKey(), msg, bh.Sig) {
		t.Fatalf("bridge header does not verify: %v", err)
	}
}

func TestVectorFrames(t *testing.T) {
	v := loadVectors(t)
	k := v.KeyDerivation
	keys := map[string][]byte{"deviceToBridge": mustHex(t, k.DeviceToBridgeKeyHex), "bridgeToDevice": mustHex(t, k.BridgeToDeviceKeyHex)}
	sealers := map[string]*Sealer{}
	openers := map[string]*Opener{}
	for dir, key := range keys {
		s, err := NewSealer(key)
		if err != nil {
			t.Fatal(err)
		}
		o, err := NewOpener(key)
		if err != nil {
			t.Fatal(err)
		}
		sealers[dir], openers[dir] = s, o
	}
	for _, f := range v.Frames {
		sealed, err := sealers[f.Direction].Seal(mustHex(t, f.PlaintextHex))
		if err != nil {
			t.Fatal(err)
		}
		eqString(t, f.Direction+" frame "+f.Counter, hex.EncodeToString(sealed), f.SealedHex)
		plain, err := openers[f.Direction].Open(sealed)
		if err != nil || hex.EncodeToString(plain) != f.PlaintextHex {
			t.Fatalf("open %s %s: %v", f.Direction, f.Counter, err)
		}
	}

	// Out of order: frame 1 before frame 0 fails, and the opener stays failed.
	o, _ := NewOpener(keys["deviceToBridge"])
	if _, err := o.Open(mustHex(t, v.Frames[2].SealedHex)); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("frame with counter 1 opened first: %v", err)
	}
	if _, err := o.Open(mustHex(t, v.Frames[0].SealedHex)); !errors.Is(err, ErrIntegrity) {
		t.Fatal("opener recovered after a failure")
	}
	// Replay: the same frame twice fails the second time.
	o, _ = NewOpener(keys["deviceToBridge"])
	if _, err := o.Open(mustHex(t, v.Frames[0].SealedHex)); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Open(mustHex(t, v.Frames[0].SealedHex)); !errors.Is(err, ErrIntegrity) {
		t.Fatal("replayed frame opened")
	}
	// Wrong direction: a device frame cannot be opened with the bridge key.
	o, _ = NewOpener(keys["bridgeToDevice"])
	if _, err := o.Open(mustHex(t, v.Frames[0].SealedHex)); !errors.Is(err, ErrIntegrity) {
		t.Fatal("frame opened with the other direction's key")
	}
}

func TestEd25519IsTheRFC8032Key(t *testing.T) {
	v := loadVectors(t)
	want, _ := hex.DecodeString("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")
	if !bytes.Equal(mustB64(t, v.Keys.BridgePublicKey), want) {
		t.Fatal("vector bridge key is not the RFC 8032 test key")
	}
	if len(ed25519.PublicKey(want)) != BridgePublicKeySize {
		t.Fatal("size")
	}
}
