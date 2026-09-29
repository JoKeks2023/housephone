package signaling

import (
	"net/http"
	"strings"
	"testing"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

func TestSanitizeName(t *testing.T) {
	for in, want := range map[string]string{
		"Watch\r\x1b[2K\x1b[1A\x1b[2K":            "Watch[2K[1A[2K",
		"  iPhone von Joris  ":                    "iPhone von Joris",
		"iPad\u202Edaolnwod\u202C":                "iPaddaolnwod",
		"Uhr\u2066\u200F":                         "Uhr",
		"Küche 📞":                                 "Küche 📞",
		strings.Repeat("x", maxDeviceNameRunes+5): strings.Repeat("x", maxDeviceNameRunes),
	} {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Security review N5: names from devices must not reach the admin terminal
// with control characters.
func TestDeviceNamesAreSanitizedWhenStored(t *testing.T) {
	ts := newTestServer(t)
	phone := ts.pairDevice(t)
	res, body := ts.request(t, http.MethodPut, "/v1/device", bearer(phone),
		protocol.DeviceUpdate{DeviceName: ptr("Watch\r\x1b[2K\x1b[1A\u202E")})
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT: %d %s", res.StatusCode, body)
	}
	dev, err := ts.devices.Get(phone.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(dev.Name, "\r\x1b\u202E") || dev.Name != "Watch[2K[1A" {
		t.Fatalf("stored name %q", dev.Name)
	}
}

func ptr[T any](v T) *T { return &v }
