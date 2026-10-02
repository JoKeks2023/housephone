package hp2

import (
	"strconv"
	"strings"
)

// Administration from the app (ADR-0009). An admin request is an ordinary
// HP2 request signed with the device key, plus a second signature in the
// HP2-Admin header made with the admin key: a separate Secure Enclave key
// that signs only after Face ID. Both signatures cover the same fields,
// under different labels, so neither can stand in for the other.

// AdminHeader carries the admin key's signature (base64url, 64 bytes r‖s).
const AdminHeader = "HP2-Admin"

// AdminMessage is what the admin key signs for a request: the fields of
// AuthMessage under the label HP2-ADMIN. The request's nonce is already
// bound to the device signature and remembered by the bridge, so a
// captured admin request cannot be replayed either.
func AdminMessage(in AuthInput) []byte {
	return join("HP2-ADMIN", strings.ToUpper(in.Method), in.RequestURI, in.BridgeID, in.DeviceID,
		strconv.FormatInt(in.TS, 10), in.Nonce, in.EPK, BodyHash(in.Body))
}

// AdminEnrollMessage is what the admin key signs when it is enrolled, to
// prove the device holds it. The enrolling request itself is signed by the
// device key and protected against replay by its nonce.
func AdminEnrollMessage(bridgeID, deviceID, adminKey string) []byte {
	return join("HP2-ADMIN-ENROLL", bridgeID, deviceID, adminKey)
}

// VerifyAdminRequest checks the HP2-Admin signature of a request whose
// Authorization header h was already verified.
func VerifyAdminRequest(adminKey, headerValue string, h AuthHeader, method, requestURI, bridgeID string, body []byte) bool {
	pub, err := ParseDevicePublicKey(adminKey)
	if err != nil {
		return false
	}
	sig, err := decodeSized(strings.TrimSpace(headerValue), SignatureSize)
	if err != nil {
		return false
	}
	msg := AdminMessage(AuthInput{
		Method: method, RequestURI: requestURI, BridgeID: bridgeID, DeviceID: h.DeviceID,
		TS: h.TS, Nonce: h.Nonce, EPK: h.EPK, Body: body,
	})
	return VerifyDeviceSignature(pub, msg, sig)
}

// VerifyAdminEnrollment checks the proof that the device holds adminKey.
func VerifyAdminEnrollment(adminKey, proof, bridgeID, deviceID string) bool {
	pub, err := ParseDevicePublicKey(adminKey)
	if err != nil {
		return false
	}
	sig, err := decodeSized(proof, SignatureSize)
	if err != nil {
		return false
	}
	return VerifyDeviceSignature(pub, AdminEnrollMessage(bridgeID, deviceID, adminKey), sig)
}

// AdminSignature signs a request with the admin key (device side: the
// probe and tests). The fields must be those of the request's
// Authorization header.
func (c *ClientRequest) AdminSignature(adminKey DeviceKey, method, requestURI string, body []byte) (string, error) {
	sig, err := adminKey.Sign(AdminMessage(AuthInput{
		Method: method, RequestURI: requestURI, BridgeID: c.bridgeID, DeviceID: c.header.DeviceID,
		TS: c.header.TS, Nonce: c.header.Nonce, EPK: c.header.EPK, Body: body,
	}))
	if err != nil {
		return "", err
	}
	return B64(sig), nil
}

// AdminEnrollment is the body of POST /v1/admin/enroll for adminKey
// (device side: the probe and tests).
func AdminEnrollment(adminKey DeviceKey, bridgeID, deviceID string) (publicKey, proof string, err error) {
	publicKey = B64(adminKey.PublicKeyX963())
	sig, err := adminKey.Sign(AdminEnrollMessage(bridgeID, deviceID, publicKey))
	if err != nil {
		return "", "", err
	}
	return publicKey, B64(sig), nil
}
