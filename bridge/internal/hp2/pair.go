package hp2

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// ErrWrongBridge reports a pairing answer from a bridge whose key does not
// match the fingerprint in the pairing link, or whose signature is invalid.
var ErrWrongBridge = errors.New("hp2: bridge does not match the pairing link")

// NewPairRequest builds the body of POST /v1/pair: canonical code, fresh
// nonce and the device's proof of key possession.
func NewPairRequest(key DeviceKey, code, deviceName, platform, model string) (protocol.PairRequest, error) {
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return protocol.PairRequest{}, err
	}
	req := protocol.PairRequest{
		Code:       NormalizeCode(code),
		DeviceName: deviceName,
		Platform:   platform,
		Model:      model,
		PublicKey:  B64(key.PublicKeyX963()),
		Nonce:      B64(nonce),
	}
	proof, err := key.Sign(PairProofMessage(req.Code, req.Nonce, req.PublicKey))
	if err != nil {
		return protocol.PairRequest{}, err
	}
	req.Proof = B64(proof)
	return req, nil
}

// VerifyPairResponse checks the bridge key against the fingerprint from the
// pairing link and the bridge's signature over the pairing, and returns the
// key to pin.
func VerifyPairResponse(req protocol.PairRequest, res protocol.PairResponse, fingerprint string) (ed25519.PublicKey, error) {
	raw, err := decodeSized(res.BridgePublicKey, BridgePublicKeySize)
	if err != nil {
		return nil, ErrWrongBridge
	}
	pub := ed25519.PublicKey(raw)
	if Fingerprint(pub) != fingerprint {
		return nil, ErrWrongBridge
	}
	sig, err := decodeSized(res.Signature, ed25519.SignatureSize)
	if err != nil {
		return nil, ErrWrongBridge
	}
	msg := PairResponseMessage(res.BridgeID, res.DeviceID, req.PublicKey, req.Nonce, NormalizeCode(req.Code))
	if !VerifyBridgeSignature(pub, msg, sig) {
		return nil, ErrWrongBridge
	}
	return pub, nil
}

// VerifyPairProof checks a pairing request on the bridge: well-formed key
// and nonce and a valid proof over the canonical code. It returns the
// canonical code.
func VerifyPairProof(req protocol.PairRequest) (string, error) {
	pub, err := ParseDevicePublicKey(req.PublicKey)
	if err != nil {
		return "", err
	}
	if _, err := decodeSized(req.Nonce, NonceSize); err != nil {
		return "", err
	}
	sig, err := decodeSized(req.Proof, SignatureSize)
	if err != nil {
		return "", err
	}
	code := NormalizeCode(req.Code)
	if !VerifyDeviceSignature(pub, PairProofMessage(code, req.Nonce, req.PublicKey), sig) {
		return "", errors.New("hp2: pairing proof does not verify")
	}
	return code, nil
}

// SignPairResponse fills in the bridge key and signature of a pairing
// answer.
func SignPairResponse(id *Identity, req protocol.PairRequest, code string, res protocol.PairResponse) protocol.PairResponse {
	res.BridgePublicKey = B64(id.PublicKey())
	res.Signature = B64(id.Sign(PairResponseMessage(res.BridgeID, res.DeviceID, req.PublicKey, req.Nonce, code)))
	return res
}
