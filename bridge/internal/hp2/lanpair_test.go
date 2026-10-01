package hp2

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// lanVectors mirrors docs/protocol/fixtures/crypto/lan-pairing-vectors.json.
type lanVectors struct {
	IDs   map[string]string `json:"ids"`
	URLs  map[string]string `json:"urls"`
	Keys  map[string]string `json:"keys"`
	Steps map[string]string `json:"steps"`
}

func loadLanVectors(t *testing.T) lanVectors {
	t.Helper()
	data, err := os.ReadFile("../../../docs/protocol/fixtures/crypto/lan-pairing-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v lanVectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLanPairingVectors(t *testing.T) {
	v := loadLanVectors(t)
	k, st := v.Keys, v.Steps
	bridgeID, pairingID := v.IDs["bridgeId"], v.IDs["pairingId"]

	if got := LanCommitment(k["devicePublicKeyX963"], k["deviceEphemeralPublic"], k["deviceNonce"]); got != st["commitment"] {
		t.Fatalf("commitment %s, want %s", got, st["commitment"])
	}
	if !CheckLanCommitment(st["commitment"], k["devicePublicKeyX963"], k["deviceEphemeralPublic"], k["deviceNonce"]) ||
		CheckLanCommitment(st["commitment"], k["devicePublicKeyX963"], k["bridgeEphemeralPublic"], k["deviceNonce"]) {
		t.Fatal("CheckLanCommitment")
	}
	offer := LanOfferMessage(pairingID, bridgeID, k["devicePublicKeyX963"], st["commitment"], k["bridgePublicKey"], k["bridgeEphemeralPublic"], k["bridgeNonce"])
	if string(offer) != st["offerMessage"] {
		t.Fatalf("offer message %q", offer)
	}
	seed := unhex(t, "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	id, err := NewIdentity(seed)
	if err != nil {
		t.Fatal(err)
	}
	if B64(id.Sign(offer)) != st["offerSignature"] {
		t.Fatal("offer signature differs")
	}
	tr := LanTranscript{
		PairingID: pairingID, BridgeID: bridgeID, PublicKey: k["devicePublicKeyX963"], Commitment: st["commitment"],
		BridgePublicKey: k["bridgePublicKey"], BridgeEphemeral: k["bridgeEphemeralPublic"], BridgeNonce: k["bridgeNonce"],
		DeviceEphemeral: k["deviceEphemeralPublic"], DeviceNonce: k["deviceNonce"],
	}
	hash := tr.Hash()
	if hex.EncodeToString(hash) != st["transcriptHashHex"] {
		t.Fatal("transcript hash differs")
	}
	if string(LanProofMessage(hash)) != st["proofMessage"] {
		t.Fatal("proof message differs")
	}
	pub, err := ParseDevicePublicKey(k["devicePublicKeyX963"])
	if err != nil {
		t.Fatal(err)
	}
	proof, _ := DecodeB64(st["proofSignature"])
	if !VerifyDeviceSignature(pub, LanProofMessage(hash), proof) {
		t.Fatal("proof does not verify")
	}

	devEph, _ := ecdh.X25519().NewPrivateKey(unhex(t, k["deviceEphemeralPrivateX25519Hex"]))
	brEph, _ := ecdh.X25519().NewPrivateKey(unhex(t, k["bridgeEphemeralPrivateX25519Hex"]))
	peer, err := ParseEphemeral(k["deviceEphemeralPublic"])
	if err != nil {
		t.Fatal(err)
	}
	shared, err := LanShared(brEph, peer)
	if err != nil || hex.EncodeToString(shared) != st["sharedSecretHex"] {
		t.Fatalf("shared %x, %v", shared, err)
	}
	secrets, err := DeriveLanSecrets(shared, hash)
	if err != nil {
		t.Fatal(err)
	}
	if secrets.SAS != st["sas"] || hex.EncodeToString(secrets.SealKey) != st["sealKeyHex"] {
		t.Fatalf("secrets %+v", secrets)
	}

	approved := LanApprovedMessage(hash, v.IDs["deviceId"], bridgeID, v.URLs["publicUrl"], v.URLs["lanUrl"])
	if string(approved) != st["approvedMessage"] || B64(id.Sign(approved)) != st["approvedSignature"] {
		t.Fatal("approval message or signature differs")
	}
	sealed := unhex(t, st["approvalSealedHex"])
	plain, err := OpenLan(secrets.SealKey, sealed)
	if err != nil || string(plain) != st["approvalPlaintext"] {
		t.Fatalf("open approval: %q, %v", plain, err)
	}
	if again, _ := SealLan(secrets.SealKey, plain); !bytes.Equal(again, sealed) {
		t.Fatal("sealing differs")
	}

	// The device side end to end with the fixed ephemeral key and nonce.
	key, err := SoftwareKeyFromScalar(unhex(t, "c9afa9d845ba75166b5c215767b1d6934e50c3db36e89b127b8a622b120f6721"))
	if err != nil {
		t.Fatal(err)
	}
	c := newLanClient(key, devEph, k["deviceNonce"], "iPhone", protocol.PlatformIOS, "")
	if c.Start().Commitment != st["commitment"] {
		t.Fatal("client commitment differs")
	}
	reveal, err := c.Accept(protocol.LanPairOffer{
		PairingID: pairingID, BridgeID: bridgeID, BridgePublicKey: k["bridgePublicKey"],
		BridgeEphemeral: k["bridgeEphemeralPublic"], BridgeNonce: k["bridgeNonce"], Signature: st["offerSignature"],
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.SAS() != st["sas"] || reveal.DeviceEphemeral != k["deviceEphemeralPublic"] {
		t.Fatalf("client SAS %s", c.SAS())
	}
	a, err := c.Approval(protocol.LanPairState{Status: protocol.LanPairApproved, Sealed: st["approvalSealed"]})
	if err != nil || a.DeviceID != v.IDs["deviceId"] || a.LanURL != v.URLs["lanUrl"] {
		t.Fatalf("approval %+v, %v", a, err)
	}
	if !c.BridgePublicKey().Equal(ed25519.PublicKey(id.PublicKey())) {
		t.Fatal("pinned key differs")
	}
}

// An offer whose signature does not cover the device's commitment (a
// bridge or relay that changed anything) is refused.
func TestLanClientRejectsForgedOffer(t *testing.T) {
	key, _ := NewSoftwareKey()
	c, err := NewLanClient(key, "iPhone", protocol.PlatformIOS, "")
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := NewIdentitySeed()
	id, _ := NewIdentity(seed)
	eph, _ := NewEphemeral()
	offer := protocol.LanPairOffer{
		PairingID: "AAAAAAAAAAAAAAAAAAAAAA", BridgeID: "b", BridgePublicKey: B64(id.PublicKey()),
		BridgeEphemeral: B64(eph.PublicKey().Bytes()), BridgeNonce: B64(make([]byte, NonceSize)),
	}
	offer.Signature = B64(id.Sign(LanOfferMessage(offer.PairingID, offer.BridgeID, B64(key.PublicKeyX963()),
		LanCommitment("other", "values", "here"), offer.BridgePublicKey, offer.BridgeEphemeral, offer.BridgeNonce)))
	if _, err := c.Accept(offer); err != ErrWrongBridge {
		t.Fatalf("forged offer: %v", err)
	}
}

func TestFormatSAS(t *testing.T) {
	for v, want := range map[uint32]string{0: "000000", 42: "000042", 999_999: "999999", 1_000_000: "000000", 4294967295: "967295"} {
		if got := FormatSAS(v); got != want {
			t.Errorf("FormatSAS(%d) = %s, want %s", v, got, want)
		}
	}
}
