// Package admin is the bridge's local administration interface: an HTTP
// API on a Unix socket in the data directory (no network listener) used by
// the CLI and the TUI. Access is controlled by file permissions: whoever
// can open /data/admin.sock can already read the bridge's data.
package admin

import (
	"context"
	"errors"
	"time"
)

// SocketName is the admin socket inside the data directory.
const SocketName = "admin.sock"

// ErrNotFound is returned for unknown devices or codes.
var ErrNotFound = errors.New("not found")

// Status is the overview of a running bridge.
type Status struct {
	Version     string    `json:"version"`
	BridgeName  string    `json:"bridgeName"`
	BridgeID    string    `json:"bridgeId"`
	Fingerprint string    `json:"fingerprint"`
	StartedAt   time.Time `json:"startedAt"`
	PublicURL   string    `json:"publicUrl"`
	Listen      string    `json:"listen"`

	SIPRegistered bool   `json:"sipRegistered"`
	Registrar     string `json:"registrar"`
	SIPUser       string `json:"sipUser"`

	PublicIP       string `json:"publicIp"`
	PublicIPSource string `json:"publicIpSource"`
	MediaPort      int    `json:"mediaPort"`

	APNsConfigured bool      `json:"apnsConfigured"`
	APNsTopic      string    `json:"apnsTopic"`
	LastPush       *PushInfo `json:"lastPush,omitempty"`

	FritzBoxConfigured bool     `json:"fritzBoxConfigured"`
	FritzBoxFeatures   []string `json:"fritzBoxFeatures"`

	DevicesTotal  int `json:"devicesTotal"`
	DevicesOnline int `json:"devicesOnline"`
	ActiveCalls   int `json:"activeCalls"`
}

// DeviceInfo is a paired device.
type DeviceInfo struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Platform       string    `json:"platform"`
	Model          string    `json:"model,omitempty"`
	KeyFingerprint string    `json:"keyFingerprint"`
	Media          string    `json:"media"` // "webrtc" | "websocket-pcma"
	Push           string    `json:"push"`  // "", "development", "production"
	PairedBy       string    `json:"pairedBy,omitempty"`
	PairedByName   string    `json:"pairedByName,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	LastSeen       time.Time `json:"lastSeen,omitzero"`
	Online         bool      `json:"online"`
	OnlineSince    time.Time `json:"onlineSince,omitzero"`
}

// RemoveResult lists what devices remove took away.
type RemoveResult struct {
	Removed []DeviceInfo `json:"removed"`
	Kept    []DeviceInfo `json:"kept,omitempty"`
}

// PairingInfo is a fresh one-time pairing code.
type PairingInfo struct {
	Code        string    `json:"code"`
	Grouped     string    `json:"grouped"`
	Link        string    `json:"link"`
	Fingerprint string    `json:"fingerprint"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// PairingState answers the wait for a pairing code.
type PairingState struct {
	Used    bool        `json:"used"`
	Expired bool        `json:"expired"`
	Device  *DeviceInfo `json:"device,omitempty"`
}

// CallsView is the call list.
type CallsView struct {
	Active []CallInfo `json:"active"`
	Recent []CallInfo `json:"recent"`
}

// LogsView is a slice of the log ring.
type LogsView struct {
	Lines []LogLine `json:"lines"`
	Next  uint64    `json:"next"`
}

// CheckState is the traffic light of a self-test check.
type CheckState string

const (
	CheckOK   CheckState = "ok"
	CheckWarn CheckState = "warn"
	CheckFail CheckState = "fail"
)

// Check is one self-test result with a hint what to do.
type Check struct {
	Name   string     `json:"name"`
	State  CheckState `json:"state"`
	Detail string     `json:"detail"`
	Hint   string     `json:"hint,omitempty"`
}

// Service is what the running bridge offers the admin API.
type Service interface {
	Status() Status
	Devices() ([]DeviceInfo, error)
	RenameDevice(id, name string) (DeviceInfo, error)
	RemoveDevice(id string, keepCompanions bool) (RemoveResult, error)
	CreatePairing(name string) (PairingInfo, error)
	PairingState(code string) (PairingState, error)
	RevokePairing(code string) error
	Calls() CallsView
	Stats() Stats
	Logs(after uint64) LogsView
	SelfTest(ctx context.Context) []Check
	Config() map[string]any
}
