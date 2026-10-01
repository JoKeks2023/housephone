package protocol

import "time"

// Pairing in the home network without a QR code (v2.1, ADR-0007). All
// keys, nonces and signatures are base64url without padding.

// LanPairState.Status values.
const (
	LanPairPending  = "pending"
	LanPairApproved = "approved"
	LanPairDenied   = "denied"
	LanPairExpired  = "expired"
)

// LanPairStart is the body of POST /v1/pair/lan: the device's key and its
// commitment to the ephemeral key and nonce it reveals in step 3.
type LanPairStart struct {
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
	Model      string `json:"model,omitempty"`
	PublicKey  string `json:"publicKey"`
	Commitment string `json:"commitment"`
}

// LanPairOffer answers POST /v1/pair/lan. Signature is the bridge's
// Ed25519 signature over hp2.LanOfferMessage.
type LanPairOffer struct {
	PairingID       string    `json:"pairingId"`
	BridgeID        string    `json:"bridgeId"`
	BridgeName      string    `json:"bridgeName"`
	BridgePublicKey string    `json:"bridgePublicKey"`
	BridgeEphemeral string    `json:"bridgeEphemeral"`
	BridgeNonce     string    `json:"bridgeNonce"`
	ExpiresAt       time.Time `json:"expiresAt"`
	Signature       string    `json:"signature"`
}

// LanPairReveal is the body of POST /v1/pair/lan/{pairingId}/reveal.
// Proof is the device's signature over hp2.LanProofMessage.
type LanPairReveal struct {
	DeviceEphemeral string `json:"deviceEphemeral"`
	DeviceNonce     string `json:"deviceNonce"`
	Proof           string `json:"proof"`
}

// LanPairState answers the reveal and GET /v1/pair/lan/{pairingId}.
// Sealed is set once approved: LanPairApproval sealed with the key from
// hp2.DeriveLanSecrets.
type LanPairState struct {
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	Sealed    string    `json:"sealed,omitempty"`
}

// LanPairApproval is the sealed result of an approved pairing. Signature
// is the bridge's signature over hp2.LanApprovedMessage.
type LanPairApproval struct {
	DeviceID   string `json:"deviceId"`
	BridgeID   string `json:"bridgeId"`
	BridgeName string `json:"bridgeName"`
	PublicURL  string `json:"publicUrl"`
	LanURL     string `json:"lanUrl"`
	Signature  string `json:"signature"`
}
