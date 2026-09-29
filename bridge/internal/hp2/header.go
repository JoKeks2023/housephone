package hp2

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// maxHeaderLength bounds the Authorization and HP2-Bridge values.
const maxHeaderLength = 1024

// AuthHeader is a parsed "Authorization: HP2 …" value.
type AuthHeader struct {
	DeviceID string
	TS       int64
	// Nonce and EPK keep their base64url form (as signed); NonceBytes and
	// EPKBytes are decoded.
	Nonce      string
	EPK        string
	NonceBytes []byte
	EPKBytes   []byte
	Sig        []byte
}

// String formats the header value.
func (a AuthHeader) String() string {
	return fmt.Sprintf("%s id=%s, ts=%d, nonce=%s, epk=%s, sig=%s", Scheme, a.DeviceID, a.TS, a.Nonce, a.EPK, B64(a.Sig))
}

// ParseAuthorization parses an "HP2 id=…, ts=…, nonce=…, epk=…, sig=…"
// value strictly: all five parameters exactly once, nothing else, canonical
// lower-case UUID, decimal ts and exact field sizes.
func ParseAuthorization(value string) (AuthHeader, error) {
	if len(value) > maxHeaderLength {
		return AuthHeader{}, ErrMalformed
	}
	scheme, rest, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(scheme, Scheme) {
		return AuthHeader{}, ErrMalformed
	}
	params, err := parseParams(rest, "id", "ts", "nonce", "epk", "sig")
	if err != nil {
		return AuthHeader{}, err
	}
	id, err := uuid.Parse(params["id"])
	if err != nil || id.String() != params["id"] {
		return AuthHeader{}, ErrMalformed
	}
	ts, err := parseTS(params["ts"])
	if err != nil {
		return AuthHeader{}, err
	}
	nonce, err := decodeSized(params["nonce"], NonceSize)
	if err != nil {
		return AuthHeader{}, err
	}
	epk, err := decodeSized(params["epk"], EphemeralKeySize)
	if err != nil {
		return AuthHeader{}, err
	}
	sig, err := decodeSized(params["sig"], SignatureSize)
	if err != nil {
		return AuthHeader{}, err
	}
	return AuthHeader{
		DeviceID: params["id"], TS: ts,
		Nonce: params["nonce"], EPK: params["epk"],
		NonceBytes: nonce, EPKBytes: epk, Sig: sig,
	}, nil
}

// BridgeHeaderValue is a parsed HP2-Bridge value.
type BridgeHeaderValue struct {
	EPK      string
	EPKBytes []byte
	Sig      []byte
}

// FormatBridgeHeader formats "epk=…, sig=…".
func FormatBridgeHeader(epk, sig []byte) string {
	return "epk=" + B64(epk) + ", sig=" + B64(sig)
}

// ParseBridgeHeader parses an HP2-Bridge value strictly.
func ParseBridgeHeader(value string) (BridgeHeaderValue, error) {
	if len(value) > maxHeaderLength {
		return BridgeHeaderValue{}, ErrMalformed
	}
	params, err := parseParams(value, "epk", "sig")
	if err != nil {
		return BridgeHeaderValue{}, err
	}
	epk, err := decodeSized(params["epk"], EphemeralKeySize)
	if err != nil {
		return BridgeHeaderValue{}, err
	}
	sig, err := decodeSized(params["sig"], SignatureSize)
	if err != nil {
		return BridgeHeaderValue{}, err
	}
	return BridgeHeaderValue{EPK: params["epk"], EPKBytes: epk, Sig: sig}, nil
}

// parseParams splits "k=v, k=v" and requires exactly the given keys.
func parseParams(s string, keys ...string) (map[string]string, error) {
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !want[k] || v == "" {
			return nil, ErrMalformed
		}
		if _, dup := out[k]; dup {
			return nil, ErrMalformed
		}
		out[k] = v
	}
	if len(out) != len(keys) {
		return nil, ErrMalformed
	}
	return out, nil
}

func parseTS(s string) (int64, error) {
	if len(s) == 0 || len(s) > 12 {
		return 0, ErrMalformed
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, ErrMalformed
		}
	}
	return strconv.ParseInt(s, 10, 64)
}
