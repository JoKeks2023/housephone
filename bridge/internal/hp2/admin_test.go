package hp2

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// adminVectors mirrors docs/protocol/fixtures/crypto/admin-vectors.json.
type adminVectors struct {
	IDs struct {
		BridgeID string `json:"bridgeId"`
		DeviceID string `json:"deviceId"`
	} `json:"ids"`
	Keys struct {
		AdminPrivateScalarHex string `json:"adminPrivateScalarHex"`
		AdminPublicKeyX963    string `json:"adminPublicKeyX963"`
	} `json:"keys"`
	Request struct {
		Method, Path, Body, TS, Nonce, EPK string
		AdminMessage                       string `json:"adminMessage"`
		AdminSignature                     string `json:"adminSignature"`
	} `json:"request"`
	Enroll struct {
		EnrollMessage string `json:"enrollMessage"`
		Proof         string `json:"proof"`
	} `json:"enroll"`
}

func TestAdminVectors(t *testing.T) {
	data, err := os.ReadFile("../../../docs/protocol/fixtures/crypto/admin-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v adminVectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	scalar, _ := hex.DecodeString(v.Keys.AdminPrivateScalarHex)
	key, err := SoftwareKeyFromScalar(scalar)
	if err != nil || B64(key.PublicKeyX963()) != v.Keys.AdminPublicKeyX963 {
		t.Fatalf("admin key %v", err)
	}
	ts, _ := strconv.ParseInt(v.Request.TS, 10, 64)
	in := AuthInput{Method: v.Request.Method, RequestURI: v.Request.Path, BridgeID: v.IDs.BridgeID, DeviceID: v.IDs.DeviceID,
		TS: ts, Nonce: v.Request.Nonce, EPK: v.Request.EPK, Body: []byte(v.Request.Body)}
	if got := string(AdminMessage(in)); got != v.Request.AdminMessage {
		t.Fatalf("admin message\n%q\nwant\n%q", got, v.Request.AdminMessage)
	}
	h := AuthHeader{DeviceID: v.IDs.DeviceID, TS: ts, Nonce: v.Request.Nonce, EPK: v.Request.EPK}
	if !VerifyAdminRequest(v.Keys.AdminPublicKeyX963, v.Request.AdminSignature, h, v.Request.Method, v.Request.Path, v.IDs.BridgeID, []byte(v.Request.Body)) {
		t.Fatal("vector admin signature rejected")
	}
	// The same signature over a different body, path or as AUTH fails.
	if VerifyAdminRequest(v.Keys.AdminPublicKeyX963, v.Request.AdminSignature, h, v.Request.Method, v.Request.Path, v.IDs.BridgeID, []byte(`{"name":"Flur"}`)) ||
		VerifyAdminRequest(v.Keys.AdminPublicKeyX963, v.Request.AdminSignature, h, v.Request.Method, "/v1/admin/devices/x", v.IDs.BridgeID, []byte(v.Request.Body)) {
		t.Fatal("admin signature accepted for another request")
	}
	pub, _ := ParseDevicePublicKey(v.Keys.AdminPublicKeyX963)
	sig, _ := DecodeB64(v.Request.AdminSignature)
	if VerifyDeviceSignature(pub, AuthMessage(in), sig) {
		t.Fatal("admin signature valid as HP2-AUTH: labels must separate them")
	}

	if got := string(AdminEnrollMessage(v.IDs.BridgeID, v.IDs.DeviceID, v.Keys.AdminPublicKeyX963)); got != v.Enroll.EnrollMessage {
		t.Fatalf("enroll message %q", got)
	}
	if !VerifyAdminEnrollment(v.Keys.AdminPublicKeyX963, v.Enroll.Proof, v.IDs.BridgeID, v.IDs.DeviceID) {
		t.Fatal("vector enrollment proof rejected")
	}
	if VerifyAdminEnrollment(v.Keys.AdminPublicKeyX963, v.Enroll.Proof, v.IDs.BridgeID, "other-device") {
		t.Fatal("proof accepted for another device")
	}
	// Our own client produces proofs the bridge accepts.
	p, proof, err := AdminEnrollment(key, v.IDs.BridgeID, v.IDs.DeviceID)
	if err != nil || p != v.Keys.AdminPublicKeyX963 || !VerifyAdminEnrollment(p, proof, v.IDs.BridgeID, v.IDs.DeviceID) {
		t.Fatalf("own enrollment %v", err)
	}
}
