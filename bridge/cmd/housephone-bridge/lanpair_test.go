package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

// lanService implements the admin calls of devices pending|approve|deny;
// the embedded interface panics on anything else.
type lanService struct {
	admin.Service
	requests []admin.LanPairingRequest
	approved []string
	denied   []string
	profiles []admin.ProfileInfo
}

func (s *lanService) LanPairings() []admin.LanPairingRequest { return s.requests }
func (s *lanService) Profiles() []admin.ProfileInfo          { return s.profiles }
func (s *lanService) ApproveLanPairing(id, profile string) (admin.DeviceInfo, error) {
	if profile != "" {
		id += "/" + profile
	}
	s.approved = append(s.approved, id)
	return admin.DeviceInfo{ID: "dev-1", Name: "iPhone Test", Platform: "ios"}, nil
}
func (s *lanService) DenyLanPairing(id string) error {
	s.denied = append(s.denied, id)
	return nil
}

func lanClient(t *testing.T, svc admin.Service) *admin.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hplan")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	srv, err := admin.Listen(dir, svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close(context.Background()) })
	c, err := admin.Dial(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLanPairingCommands(t *testing.T) {
	svc := &lanService{requests: []admin.LanPairingRequest{{
		ID: "Abc123xyz", DeviceName: "iPhone Test", Model: "iPhone17,1", IP: "192.168.0.30", SAS: "123456",
		KeyFingerprint: "ABCDEF", ExpiresAt: time.Now().Add(time.Minute),
	}}}
	c := lanClient(t, svc)
	var out bytes.Buffer
	if err := lanPairing(c, []string{"pending"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "123 456") || !strings.Contains(out.String(), "192.168.0.30") {
		t.Fatalf("pending:\n%s", out.String())
	}

	// Answering no does not approve.
	out.Reset()
	if err := lanPairing(c, []string{"approve", "Abc1"}, strings.NewReader("n\n"), &out); err != nil || len(svc.approved) != 0 {
		t.Fatalf("approve with no: %v %v", err, svc.approved)
	}
	// A wrong -code does not approve either.
	if err := lanPairing(c, []string{"approve", "-code", "654321", "Abc123xyz"}, nil, &out); err == nil || len(svc.approved) != 0 {
		t.Fatalf("approve with wrong code: %v %v", err, svc.approved)
	}
	if err := lanPairing(c, []string{"approve", "Abc123xyz"}, strings.NewReader("j\n"), &out); err != nil || len(svc.approved) != 1 {
		t.Fatalf("approve: %v %v", err, svc.approved)
	}
	if err := lanPairing(c, []string{"approve", "-code", "123 456", "Abc123xyz"}, nil, &out); err != nil || len(svc.approved) != 2 {
		t.Fatalf("approve with code: %v %v", err, svc.approved)
	}
	if err := lanPairing(c, []string{"deny", "Abc123xyz"}, nil, &out); err != nil || len(svc.denied) != 1 {
		t.Fatalf("deny: %v %v", err, svc.denied)
	}
	if err := lanPairing(c, []string{"deny", "nope"}, nil, &out); err == nil {
		t.Fatal("deny of an unknown request succeeded")
	}
}
