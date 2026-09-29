package hp2

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"time"
)

// VerifyRequest checks the device's signature over a request. The clock
// window and nonce reuse are the caller's to check (after this succeeded,
// so garbage cannot fill the nonce cache).
func VerifyRequest(pub *ecdsa.PublicKey, h AuthHeader, method, requestURI, bridgeID string, body []byte) bool {
	msg := AuthMessage(AuthInput{
		Method: method, RequestURI: requestURI, BridgeID: bridgeID, DeviceID: h.DeviceID,
		TS: h.TS, Nonce: h.Nonce, EPK: h.EPK, Body: body,
	})
	return VerifyDeviceSignature(pub, msg, h.Sig)
}

// WithinClockSkew reports whether ts (Unix seconds) is within MaxClockSkew
// of now.
func WithinClockSkew(ts int64, now time.Time) bool {
	d := now.Sub(time.Unix(ts, 0))
	return d <= MaxClockSkew && d >= -MaxClockSkew
}

// ServerSession is the bridge side of one request: its ephemeral key and
// the derived session keys.
type ServerSession struct {
	BridgeID string
	Header   AuthHeader
	eph      *ecdh.PrivateKey
	Keys     SessionKeys
}

// Accept starts the bridge side of a request whose Authorization header
// parsed. It works for rejected requests too, so their 401 can be signed.
func Accept(bridgeID string, h AuthHeader) (*ServerSession, error) {
	eph, err := NewEphemeral()
	if err != nil {
		return nil, err
	}
	keys, err := DeriveKeys(eph, h.EPKBytes, h.NonceBytes, KeysInfo(bridgeID, h.DeviceID, h.EPK, B64(eph.PublicKey().Bytes())))
	if err != nil {
		return nil, err
	}
	return &ServerSession{BridgeID: bridgeID, Header: h, eph: eph, Keys: keys}, nil
}

// SignAnswer returns the HP2-Bridge value for an answer with the given
// status and body exactly as sent (nil for 101, 204 and 304).
func (s *ServerSession) SignAnswer(id *Identity, status int, body []byte) string {
	epk := s.eph.PublicKey().Bytes()
	msg := BridgeMessage(BridgeInput{
		BridgeID: s.BridgeID, DeviceID: s.Header.DeviceID, Nonce: s.Header.Nonce,
		DeviceEPK: s.Header.EPK, BridgeEPK: B64(epk), Status: status, Body: body,
	})
	return FormatBridgeHeader(epk, id.Sign(msg))
}

// ErrUntrustedBridge reports an answer that is not signed by the pinned
// bridge key.
var ErrUntrustedBridge = errors.New("hp2: answer not signed by the paired bridge")

// ClientRequest is the device side of one request.
type ClientRequest struct {
	header   AuthHeader
	eph      *ecdh.PrivateKey
	bridgeID string
}

// NewClientRequest signs a request. requestURI is the path including the
// query exactly as sent.
func NewClientRequest(key DeviceKey, deviceID, bridgeID, method, requestURI string, body []byte, now time.Time) (*ClientRequest, error) {
	eph, err := NewEphemeral()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	h := AuthHeader{
		DeviceID: deviceID, TS: now.Unix(),
		Nonce: B64(nonce), NonceBytes: nonce,
		EPK: B64(eph.PublicKey().Bytes()), EPKBytes: eph.PublicKey().Bytes(),
	}
	sig, err := key.Sign(AuthMessage(AuthInput{
		Method: method, RequestURI: requestURI, BridgeID: bridgeID, DeviceID: deviceID,
		TS: h.TS, Nonce: h.Nonce, EPK: h.EPK, Body: body,
	}))
	if err != nil {
		return nil, err
	}
	h.Sig = sig
	return &ClientRequest{header: h, eph: eph, bridgeID: bridgeID}, nil
}

// Authorization is the Authorization header value.
func (c *ClientRequest) Authorization() string { return c.header.String() }

// VerifyAnswer checks the HP2-Bridge header of an answer with the pinned
// bridge key and derives the session keys. body is the body exactly as
// received (sealed if sealed).
func (c *ClientRequest) VerifyAnswer(bridgePub ed25519.PublicKey, status int, body []byte, headerValue string) (SessionKeys, error) {
	if headerValue == "" {
		return SessionKeys{}, ErrUntrustedBridge
	}
	bh, err := ParseBridgeHeader(headerValue)
	if err != nil {
		return SessionKeys{}, ErrUntrustedBridge
	}
	msg := BridgeMessage(BridgeInput{
		BridgeID: c.bridgeID, DeviceID: c.header.DeviceID, Nonce: c.header.Nonce,
		DeviceEPK: c.header.EPK, BridgeEPK: bh.EPK, Status: status, Body: body,
	})
	if !VerifyBridgeSignature(bridgePub, msg, bh.Sig) {
		return SessionKeys{}, ErrUntrustedBridge
	}
	return DeriveKeys(c.eph, bh.EPKBytes, c.header.NonceBytes, KeysInfo(c.bridgeID, c.header.DeviceID, c.header.EPK, bh.EPK))
}
