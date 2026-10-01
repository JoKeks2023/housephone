package hp2

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
)

// Pairing in the home network without a QR code (HPHN-40/41, ADR-0007).
//
// The device has no fingerprint to pin, so both sides show a six-digit
// confirmation code (SAS) that an admin compares before approving. Six
// digits are only safe if nobody can choose their keys after seeing the
// other side's, so the device commits first:
//
//  1. device → bridge: device key, C = SHA-256(commit(device key, eD, nD))
//  2. bridge → device: bridge key, eB, nB, signed by the bridge identity
//  3. device → bridge: eD, nD (the bridge checks C) and a proof of the
//     device key over the transcript hash
//
// SAS = HKDF(X25519(eD, eB), salt = transcript hash, "HP2-LAN-SAS") as a
// number below 10^6. A man in the middle has to fix its values on one side
// before the other side's random values are known, so the two codes match
// with probability 10^-6 per attempt; every attempt is a pending request an
// admin sees.

// LanSASDigits is the length of the confirmation code.
const LanSASDigits = 6

// LanCommitment is the device's commitment in step 1 (base64url).
func LanCommitment(publicKey, deviceEphemeral, deviceNonce string) string {
	sum := sha256.Sum256(join("HP2-LAN-COMMIT", publicKey, deviceEphemeral, deviceNonce))
	return B64(sum[:])
}

// LanOfferMessage is what the bridge signs in step 2.
func LanOfferMessage(pairingID, bridgeID, publicKey, commitment, bridgePublicKey, bridgeEphemeral, bridgeNonce string) []byte {
	return join("HP2-LAN-OFFER", pairingID, bridgeID, publicKey, commitment, bridgePublicKey, bridgeEphemeral, bridgeNonce)
}

// LanTranscript are the public values of one pairing, all base64url.
type LanTranscript struct {
	PairingID       string
	BridgeID        string
	PublicKey       string
	Commitment      string
	BridgePublicKey string
	BridgeEphemeral string
	BridgeNonce     string
	DeviceEphemeral string
	DeviceNonce     string
}

// Hash is SHA-256 over all values; salt of the key derivation and input of
// the device's proof and the bridge's approval.
func (t LanTranscript) Hash() []byte {
	sum := sha256.Sum256(join("HP2-LAN-TRANSCRIPT", t.PairingID, t.BridgeID, t.PublicKey, t.Commitment,
		t.BridgePublicKey, t.BridgeEphemeral, t.BridgeNonce, t.DeviceEphemeral, t.DeviceNonce))
	return sum[:]
}

// LanProofMessage is what the device signs in step 3.
func LanProofMessage(transcriptHash []byte) []byte {
	return join("HP2-LAN-PROOF", B64(transcriptHash))
}

// LanApprovedMessage is what the bridge signs when an admin approved.
func LanApprovedMessage(transcriptHash []byte, deviceID, bridgeID, publicURL, lanURL string) []byte {
	return join("HP2-LAN-APPROVED", B64(transcriptHash), deviceID, bridgeID, publicURL, lanURL)
}

// LanSecrets are derived from the X25519 secret and the transcript.
type LanSecrets struct {
	// SAS is the six-digit confirmation code.
	SAS string
	// SealKey seals the approval (counter 0, AAD "HP2").
	SealKey []byte
}

// DeriveLanSecrets derives the confirmation code and the key that seals
// the approval.
func DeriveLanSecrets(shared, transcriptHash []byte) (LanSecrets, error) {
	sas, err := hkdf.Key(sha256.New, shared, transcriptHash, "HP2-LAN-SAS", 4)
	if err != nil {
		return LanSecrets{}, err
	}
	key, err := hkdf.Key(sha256.New, shared, transcriptHash, "HP2-LAN-SEAL", 32)
	if err != nil {
		return LanSecrets{}, err
	}
	return LanSecrets{SAS: FormatSAS(binary.BigEndian.Uint32(sas)), SealKey: key}, nil
}

// FormatSAS reduces 32 bits to six decimal digits (bias below 2^-12).
func FormatSAS(v uint32) string { return fmt.Sprintf("%06d", v%1_000_000) }

// CheckLanCommitment compares the revealed values against the commitment
// in constant time.
func CheckLanCommitment(commitment, publicKey, deviceEphemeral, deviceNonce string) bool {
	want := LanCommitment(publicKey, deviceEphemeral, deviceNonce)
	return subtle.ConstantTimeCompare([]byte(want), []byte(commitment)) == 1
}

// ParseEphemeral decodes a base64url X25519 public key.
func ParseEphemeral(b64 string) (*ecdh.PublicKey, error) {
	raw, err := decodeSized(b64, EphemeralKeySize)
	if err != nil {
		return nil, err
	}
	pub, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, ErrMalformed
	}
	return pub, nil
}

// LanShared is the X25519 secret; an all-zero result (low-order point) is
// rejected.
func LanShared(priv *ecdh.PrivateKey, peer *ecdh.PublicKey) ([]byte, error) {
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, ErrMalformed
	}
	return shared, nil
}

// SealLan seals the approval payload.
func SealLan(key, plaintext []byte) ([]byte, error) { return SealBody(key, plaintext) }

// OpenLan opens the approval payload.
func OpenLan(key, sealed []byte) ([]byte, error) { return OpenBody(key, sealed) }
