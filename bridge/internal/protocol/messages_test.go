package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const fixturesDir = "../../../docs/protocol/fixtures"

// payloadFor returns a pointer to the typed payload for a message type.
func payloadFor(t *testing.T, msgType string) any {
	t.Helper()
	switch msgType {
	case TypeHello:
		return &Hello{}
	case TypeDeviceUpdate:
		return &DeviceUpdate{}
	case TypeDeviceUnpair:
		return &DeviceUnpair{}
	case TypeCallAttach:
		return &CallAttach{}
	case TypeCallDial:
		return &CallDial{}
	case TypeCallAnswer:
		return &CallAnswer{}
	case TypeCallAccept:
		return &CallAccept{}
	case TypeCallHangup:
		return &CallHangup{}
	case TypeCallDTMF:
		return &CallDTMF{}
	case TypeDevicePaired:
		return &DevicePaired{}
	case TypeAdminAction:
		return &AdminAction{}
	case TypeAdminRole:
		return &AdminRole{}
	case TypeWelcome:
		return &Welcome{}
	case TypeStatus:
		return &Status{}
	case TypeCallIncoming:
		return &CallIncoming{}
	case TypeCallOffer:
		return &CallOffer{}
	case TypeCallState:
		return &CallState{}
	case TypeCallEnded:
		return &CallEnded{}
	case TypeError:
		return &Error{}
	case TypePairCompanionRequest:
		return &PairCompanionRequest{}
	case TypePairCompanion:
		return &PairCompanion{}
	case TypeCallMedia:
		return &CallMedia{}
	}
	t.Fatalf("no payload type registered for %q", msgType)
	return nil
}

func semanticEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(va, vb)
}

func TestFixturesRoundTrip(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 21 {
		t.Fatalf("expected at least 21 fixtures, found %d in %s", len(files), fixturesDir)
	}

	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		if strings.HasPrefix(name, "push.") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			env, err := ParseEnvelope(data)
			if err != nil {
				t.Fatal(err)
			}
			if env.Type != name {
				t.Fatalf("fixture %s has type %q", name, env.Type)
			}
			payload := payloadFor(t, env.Type)
			if err := env.Decode(payload); err != nil {
				t.Fatal(err)
			}
			reencoded, err := NewEnvelope(env.Type, reflect.ValueOf(payload).Elem().Interface())
			if err != nil {
				t.Fatal(err)
			}
			out, err := reencoded.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if !semanticEqual(t, data, out) {
				t.Fatalf("round trip mismatch\nfixture: %s\nencoded: %s", data, out)
			}
		})
	}
}

func TestPushFixtureRoundTrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(fixturesDir, "push.incoming_call.json"))
	if err != nil {
		t.Fatal(err)
	}
	var push PushIncomingCall
	if err := json.Unmarshal(data, &push); err != nil {
		t.Fatal(err)
	}
	built := NewPushIncomingCall(push.CallID, push.Caller, push.CallerName, push.BridgeID)
	out, err := json.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	if !semanticEqual(t, data, out) {
		t.Fatalf("push mismatch\nfixture: %s\nbuilt:   %s", data, out)
	}
}

func TestEnvelopeWithoutPayloadDecodesAsEmpty(t *testing.T) {
	env, err := ParseEnvelope([]byte(`{"type":"call.accept"}`))
	if err != nil {
		t.Fatal(err)
	}
	var accept CallAccept
	if err := env.Decode(&accept); err != nil {
		t.Fatal(err)
	}
	out, err := env.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"type":"call.accept","payload":{}}` {
		t.Fatalf("unexpected encoding %s", out)
	}
}

func TestParseEnvelopeRejectsMissingType(t *testing.T) {
	if _, err := ParseEnvelope([]byte(`{"payload":{}}`)); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseEnvelope([]byte(`not json`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnknownFieldsAreIgnored(t *testing.T) {
	env, err := ParseEnvelope([]byte(`{"type":"call.dial","payload":{"callId":"x","number":"1","future":true},"extra":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var dial CallDial
	if err := env.Decode(&dial); err != nil {
		t.Fatal(err)
	}
	if dial.CallID != "x" || dial.Number != "1" {
		t.Fatalf("unexpected %+v", dial)
	}
}

func TestTimestampHasNoFractionalSeconds(t *testing.T) {
	ts := Timestamp(time.Date(2026, 9, 29, 20, 4, 5, 123456789, time.FixedZone("CEST", 2*3600)))
	out, err := json.Marshal(CallIncoming{CallID: "c", Caller: "1", StartedAt: ts})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"startedAt":"2026-09-29T18:04:05Z"`) {
		t.Fatalf("unexpected timestamp encoding %s", out)
	}
}

// The HTTP bodies (docs/protocol/fixtures/http: v1.2 phonebook and history,
// v2 pairing) round-trip through the Go types without losing or adding
// fields.
func TestHTTPFixturesRoundTrip(t *testing.T) {
	for file, v := range map[string]any{
		"phonebook.json":     &Phonebook{},
		"history.json":       &History{},
		"pair.request.json":  &PairRequest{},
		"pair.response.json": &PairResponse{},
		// v2.1: pairing in the home network (ADR-0007).
		"pair-lan.start.json":    &LanPairStart{},
		"pair-lan.offer.json":    &LanPairOffer{},
		"pair-lan.reveal.json":   &LanPairReveal{},
		"pair-lan.pending.json":  &LanPairState{},
		"pair-lan.approved.json": &LanPairState{},
	} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixturesDir, "http", file))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, v); err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if !semanticEqual(t, data, out) {
				t.Fatalf("round trip mismatch\nfixture: %s\nencoded: %s", data, out)
			}
		})
	}
}
