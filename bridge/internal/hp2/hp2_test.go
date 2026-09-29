package hp2

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

const (
	testDevice = "9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	testBridge = "e7a1c3d5-0f2b-4d6e-8a9c-1b3d5f7a9c2e"
)

func validHeader(t *testing.T) string {
	t.Helper()
	key, _ := NewSoftwareKey()
	req, err := NewClientRequest(key, testDevice, testBridge, "GET", "/v1/ws", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return req.Authorization()
}

func TestParseAuthorizationRejectsVariants(t *testing.T) {
	good := validHeader(t)
	if _, err := ParseAuthorization(good); err != nil {
		t.Fatalf("valid header rejected: %v", err)
	}
	params := strings.TrimPrefix(good, "HP2 ")
	parts := strings.Split(params, ", ")
	cases := map[string]string{
		"empty":           "",
		"bearer":          "Bearer abc.def",
		"no params":       "HP2",
		"missing sig":     "HP2 " + strings.Join(parts[:4], ", "),
		"duplicate id":    "HP2 " + params + ", " + parts[0],
		"extra param":     "HP2 " + params + ", x=1",
		"upper-case uuid": strings.Replace(good, testDevice, strings.ToUpper(testDevice), 1),
		"not a uuid":      strings.Replace(good, testDevice, "device-1", 1),
		"negative ts":     strings.Replace(good, "ts=", "ts=-", 1),
		"short nonce":     strings.Replace(good, "nonce=", "nonce=AA", 1),
		"padded sig":      good + "==",
		"too long":        "HP2 " + strings.Repeat("a", 2000),
	}
	for name, h := range cases {
		if _, err := ParseAuthorization(h); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	// Scheme is case-insensitive (RFC 9110), parameter order is free.
	if _, err := ParseAuthorization("hp2 " + params); err != nil {
		t.Errorf("lower-case scheme rejected: %v", err)
	}
	reversed := make([]string, len(parts))
	for i, p := range parts {
		reversed[len(parts)-1-i] = p
	}
	if _, err := ParseAuthorization("HP2 " + strings.Join(reversed, ", ")); err != nil {
		t.Errorf("reordered parameters rejected: %v", err)
	}
}

func TestRequestAndAnswerRoundTrip(t *testing.T) {
	key, _ := NewSoftwareKey()
	seed, _ := NewIdentitySeed()
	id, _ := NewIdentity(seed)
	body := []byte(`{"pushToken":"ab"}`)
	now := time.Now()
	creq, err := NewClientRequest(key, testDevice, testBridge, "PUT", "/v1/device", body, now)
	if err != nil {
		t.Fatal(err)
	}

	h, err := ParseAuthorization(creq.Authorization())
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := ParseDevicePublicKey(B64(key.PublicKeyX963()))
	if !VerifyRequest(pub, h, "PUT", "/v1/device", testBridge, body) {
		t.Fatal("request does not verify")
	}
	if !WithinClockSkew(h.TS, now.Add(59*time.Second)) || WithinClockSkew(h.TS, now.Add(61*time.Second)) || WithinClockSkew(h.TS, now.Add(-61*time.Second)) {
		t.Fatal("clock window is not ±60 s")
	}
	sess, err := Accept(testBridge, h)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealBody(sess.Keys.BridgeToDevice, []byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	header := sess.SignAnswer(id, 200, sealed)

	keys, err := creq.VerifyAnswer(id.PublicKey(), 200, sealed, header)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := OpenBody(keys.BridgeToDevice, sealed)
	if err != nil || string(plain) != `{"ok":true}` {
		t.Fatalf("OpenBody = %q, %v", plain, err)
	}

	// The device rejects answers that are not exactly what the pinned
	// bridge signed.
	otherSeed, _ := NewIdentitySeed()
	other, _ := NewIdentity(otherSeed)
	for name, check := range map[string]func() error{
		"other bridge key": func() error { _, err := creq.VerifyAnswer(other.PublicKey(), 200, sealed, header); return err },
		"other status":     func() error { _, err := creq.VerifyAnswer(id.PublicKey(), 204, sealed, header); return err },
		"changed body": func() error {
			changed := append([]byte{}, sealed...)
			changed[0] ^= 1
			_, err := creq.VerifyAnswer(id.PublicKey(), 200, changed, header)
			return err
		},
		"missing header": func() error { _, err := creq.VerifyAnswer(id.PublicKey(), 200, sealed, ""); return err },
		"signed by impostor": func() error {
			_, err := creq.VerifyAnswer(id.PublicKey(), 200, sealed, sess.SignAnswer(other, 200, sealed))
			return err
		},
	} {
		if err := check(); !errors.Is(err, ErrUntrustedBridge) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPairingProofAndAnswer(t *testing.T) {
	key, _ := NewSoftwareKey()
	seed, _ := NewIdentitySeed()
	id, _ := NewIdentity(seed)
	req, err := NewPairRequest(key, "k7p2-xh9q-rmw4-dzt8", "iPhone", "ios", "iPhone17,1")
	if err != nil {
		t.Fatal(err)
	}
	if req.Code != "K7P2XH9QRMW4DZT8" {
		t.Fatalf("code not canonical: %q", req.Code)
	}
	code, err := VerifyPairProof(req)
	if err != nil {
		t.Fatal(err)
	}
	res := SignPairResponse(id, req, code, protocol.PairResponse{DeviceID: testDevice, BridgeID: testBridge, BridgeName: "Zuhause"})
	if _, err := VerifyPairResponse(req, res, id.Fingerprint()); err != nil {
		t.Fatal(err)
	}

	// Wrong fingerprint (another bridge answered) and a proof for another
	// key are rejected.
	otherSeed, _ := NewIdentitySeed()
	other, _ := NewIdentity(otherSeed)
	if _, err := VerifyPairResponse(req, res, other.Fingerprint()); !errors.Is(err, ErrWrongBridge) {
		t.Fatalf("answer accepted for another fingerprint: %v", err)
	}
	forged := SignPairResponse(other, req, code, protocol.PairResponse{DeviceID: testDevice, BridgeID: testBridge})
	if _, err := VerifyPairResponse(req, forged, id.Fingerprint()); !errors.Is(err, ErrWrongBridge) {
		t.Fatalf("answer from another bridge accepted: %v", err)
	}
	otherKey, _ := NewSoftwareKey()
	stolen := req
	stolen.PublicKey = B64(otherKey.PublicKeyX963())
	if _, err := VerifyPairProof(stolen); err == nil {
		t.Fatal("proof accepted for a key it was not made with")
	}
	notOnCurve := req
	raw := key.PublicKeyX963()
	raw[40] ^= 0xff
	notOnCurve.PublicKey = B64(raw)
	if _, err := VerifyPairProof(notOnCurve); err == nil {
		t.Fatal("point off the curve accepted")
	}
}

func TestDeriveKeysRejectsLowOrderPoint(t *testing.T) {
	eph, _ := NewEphemeral()
	if _, err := DeriveKeys(eph, make([]byte, 32), make([]byte, 16), "x"); !errors.Is(err, ErrMalformed) {
		t.Fatalf("all-zero public key accepted: %v", err)
	}
}

func TestPairingLinkRoundTrip(t *testing.T) {
	link := FormatPairingLink("wss://phone.example.com/v1/ws", "K7P2XH9QRMW4DZT8", "fp_1", "Bei Joris")
	if strings.Contains(link, "+") {
		t.Fatalf("spaces not percent-encoded: %s", link)
	}
	p, err := ParsePairingLink(link)
	if err != nil || p.Name != "Bei Joris" || p.Fingerprint != "fp_1" || p.Code != "K7P2XH9QRMW4DZT8" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := ParsePairingLink("housephone://pair?url=x&code=y&name=z"); !errors.Is(err, ErrNotPairingLink) {
		t.Fatal("v1 link accepted")
	}
	base, err := HTTPBase("wss://phone.example.com/v1/ws")
	if err != nil || base != "https://phone.example.com" {
		t.Fatalf("HTTPBase = %q, %v", base, err)
	}
	if GroupCode("K7P2XH9QRMW4DZT8") != "K7P2-XH9Q-RMW4-DZT8" {
		t.Fatal(GroupCode("K7P2XH9QRMW4DZT8"))
	}
}
