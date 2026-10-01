package hp2

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// LanClient is the device side of pairing in the home network (probe,
// tests; the apps implement the same in Swift).
type LanClient struct {
	key       DeviceKey
	publicKey string
	ephemeral *ecdh.PrivateKey
	nonce     string
	start     protocol.LanPairStart

	offer      protocol.LanPairOffer
	bridgePub  ed25519.PublicKey
	transcript LanTranscript
	secrets    LanSecrets
}

// NewLanClient prepares step 1 with a fresh ephemeral key and nonce.
func NewLanClient(key DeviceKey, deviceName, platform, model string) (*LanClient, error) {
	eph, err := NewEphemeral()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return newLanClient(key, eph, B64(nonce), deviceName, platform, model), nil
}

func newLanClient(key DeviceKey, eph *ecdh.PrivateKey, nonce, deviceName, platform, model string) *LanClient {
	pub := B64(key.PublicKeyX963())
	c := &LanClient{key: key, publicKey: pub, ephemeral: eph, nonce: nonce}
	c.start = protocol.LanPairStart{
		DeviceName: deviceName,
		Platform:   platform,
		Model:      model,
		PublicKey:  pub,
		Commitment: LanCommitment(pub, B64(eph.PublicKey().Bytes()), nonce),
	}
	return c
}

// Start is the body of step 1.
func (c *LanClient) Start() protocol.LanPairStart { return c.start }

// Accept checks the bridge's signed offer and returns the body of step 3.
// The SAS is available afterwards.
func (c *LanClient) Accept(offer protocol.LanPairOffer) (protocol.LanPairReveal, error) {
	raw, err := decodeSized(offer.BridgePublicKey, BridgePublicKeySize)
	if err != nil {
		return protocol.LanPairReveal{}, ErrWrongBridge
	}
	pub := ed25519.PublicKey(raw)
	sig, err := decodeSized(offer.Signature, ed25519.SignatureSize)
	if err != nil {
		return protocol.LanPairReveal{}, ErrWrongBridge
	}
	msg := LanOfferMessage(offer.PairingID, offer.BridgeID, c.publicKey, c.start.Commitment,
		offer.BridgePublicKey, offer.BridgeEphemeral, offer.BridgeNonce)
	if !VerifyBridgeSignature(pub, msg, sig) {
		return protocol.LanPairReveal{}, ErrWrongBridge
	}
	peer, err := ParseEphemeral(offer.BridgeEphemeral)
	if err != nil {
		return protocol.LanPairReveal{}, ErrWrongBridge
	}
	if _, err := decodeSized(offer.BridgeNonce, NonceSize); err != nil {
		return protocol.LanPairReveal{}, ErrWrongBridge
	}
	shared, err := LanShared(c.ephemeral, peer)
	if err != nil {
		return protocol.LanPairReveal{}, ErrWrongBridge
	}
	c.offer, c.bridgePub = offer, pub
	c.transcript = LanTranscript{
		PairingID: offer.PairingID, BridgeID: offer.BridgeID, PublicKey: c.publicKey, Commitment: c.start.Commitment,
		BridgePublicKey: offer.BridgePublicKey, BridgeEphemeral: offer.BridgeEphemeral, BridgeNonce: offer.BridgeNonce,
		DeviceEphemeral: B64(c.ephemeral.PublicKey().Bytes()), DeviceNonce: c.nonce,
	}
	hash := c.transcript.Hash()
	if c.secrets, err = DeriveLanSecrets(shared, hash); err != nil {
		return protocol.LanPairReveal{}, err
	}
	proof, err := c.key.Sign(LanProofMessage(hash))
	if err != nil {
		return protocol.LanPairReveal{}, err
	}
	return protocol.LanPairReveal{
		DeviceEphemeral: c.transcript.DeviceEphemeral,
		DeviceNonce:     c.nonce,
		Proof:           B64(proof),
	}, nil
}

// SAS is the confirmation code to compare with the bridge (after Accept).
func (c *LanClient) SAS() string { return c.secrets.SAS }

// BridgePublicKey is the bridge key to pin (after Accept).
func (c *LanClient) BridgePublicKey() ed25519.PublicKey { return c.bridgePub }

// Approval opens and checks the approval from an "approved" state.
func (c *LanClient) Approval(state protocol.LanPairState) (protocol.LanPairApproval, error) {
	sealed, err := DecodeB64(state.Sealed)
	if err != nil {
		return protocol.LanPairApproval{}, ErrWrongBridge
	}
	plain, err := OpenLan(c.secrets.SealKey, sealed)
	if err != nil {
		return protocol.LanPairApproval{}, ErrWrongBridge
	}
	var a protocol.LanPairApproval
	if err := json.Unmarshal(plain, &a); err != nil {
		return protocol.LanPairApproval{}, ErrWrongBridge
	}
	sig, err := decodeSized(a.Signature, ed25519.SignatureSize)
	if err != nil || a.BridgeID != c.offer.BridgeID ||
		!VerifyBridgeSignature(c.bridgePub, LanApprovedMessage(c.transcript.Hash(), a.DeviceID, a.BridgeID, a.PublicURL, a.LanURL), sig) {
		return protocol.LanPairApproval{}, ErrWrongBridge
	}
	return a, nil
}
