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

// ErrUnknownProfile is returned for a profile that is not configured.
var ErrUnknownProfile = errors.New("unbekanntes Profil")

// ErrNotAllowed is returned for an admin action that is not possible, e.g.
// making a watch admin (ADR-0009).
var ErrNotAllowed = errors.New("nicht erlaubt")

// ProfileInfo is a household profile (ADR-0008): one IP phone at the
// FRITZ!Box with its own number.
type ProfileInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	SIPUser    string   `json:"sipUser"`
	Numbers    []string `json:"numbers,omitempty"`
	Phonebooks []string `json:"phonebooks,omitempty"`
	Registered bool     `json:"registered"`
	// HistoryAllowed is false for a profile without own numbers in a
	// household with several profiles: it sees no call list.
	HistoryAllowed bool `json:"historyAllowed"`
	Devices        int  `json:"devices"`
	DevicesOnline  int  `json:"devicesOnline"`
}

// MoveResult lists the devices moved to another profile: the device and
// the watches paired through it.
type MoveResult struct {
	Moved []DeviceInfo `json:"moved"`
}

// Status is the overview of a running bridge.
type Status struct {
	Version     string    `json:"version"`
	BridgeName  string    `json:"bridgeName"`
	BridgeID    string    `json:"bridgeId"`
	Fingerprint string    `json:"fingerprint"`
	StartedAt   time.Time `json:"startedAt"`
	PublicURL   string    `json:"publicUrl"`
	Listen      string    `json:"listen"`
	// PrivateListen and LanURL are the home network listener; empty if
	// it is disabled.
	PrivateListen string `json:"privateListen,omitempty"`
	LanURL        string `json:"lanUrl,omitempty"`

	// SIPRegistered: every profile's line is registered.
	SIPRegistered bool   `json:"sipRegistered"`
	Registrar     string `json:"registrar"`
	SIPUser       string `json:"sipUser"`
	// Profiles are the household profiles, the default one first.
	Profiles []ProfileInfo `json:"profiles"`

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
	ID             string `json:"id"`
	Name           string `json:"name"`
	Platform       string `json:"platform"`
	Model          string `json:"model,omitempty"`
	KeyFingerprint string `json:"keyFingerprint"`
	Media          string `json:"media"` // "webrtc" | "websocket-pcma"
	Push           string `json:"push"`  // "", "development", "production"
	PairedBy       string `json:"pairedBy,omitempty"`
	PairedByName   string `json:"pairedByName,omitempty"`
	Profile        string `json:"profile"`
	ProfileName    string `json:"profileName"`
	// Admin may manage the bridge from the app (ADR-0009); AdminEnrolled:
	// its Face ID key is known; AdminEnrollUntil: the window to enroll it.
	Admin            bool      `json:"admin,omitempty"`
	AdminEnrolled    bool      `json:"adminEnrolled,omitempty"`
	AdminEnrollUntil time.Time `json:"adminEnrollUntil,omitzero"`
	CreatedAt        time.Time `json:"createdAt"`
	LastSeen         time.Time `json:"lastSeen,omitzero"`
	Online           bool      `json:"online"`
	OnlineSince      time.Time `json:"onlineSince,omitzero"`
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
	Profile     string    `json:"profile"`
	ProfileName string    `json:"profileName"`
	Fingerprint string    `json:"fingerprint"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// PairingState answers the wait for a pairing code.
type PairingState struct {
	Used    bool        `json:"used"`
	Expired bool        `json:"expired"`
	Device  *DeviceInfo `json:"device,omitempty"`
}

// LanPairingRequest is a device in the home network that asked to pair
// without a QR code (ADR-0007) and waits for an admin. The admin approves
// only if the device shows the same SAS.
type LanPairingRequest struct {
	ID             string    `json:"id"`
	DeviceName     string    `json:"deviceName"`
	Platform       string    `json:"platform"`
	Model          string    `json:"model,omitempty"`
	IP             string    `json:"ip"`
	SAS            string    `json:"sas"`
	KeyFingerprint string    `json:"keyFingerprint"`
	CreatedAt      time.Time `json:"createdAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

// GroupSAS formats a confirmation code as "123 456".
func GroupSAS(sas string) string {
	if len(sas) != 6 {
		return sas
	}
	return sas[:3] + " " + sas[3:]
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
	// Profiles lists the household profiles (ADR-0008).
	Profiles() []ProfileInfo
	Devices() ([]DeviceInfo, error)
	RenameDevice(id, name string) (DeviceInfo, error)
	RemoveDevice(id string, keepCompanions bool) (RemoveResult, error)
	// MoveDevice moves a device and its watches to another profile and
	// reconnects them.
	MoveDevice(id, profile string) (MoveResult, error)
	// PromoteDevice makes an iPhone admin and opens the window in which
	// it enrolls its Face ID key; on an admin it reopens the window (new
	// key after a Face ID change). ErrNotAllowed for a watch.
	PromoteDevice(id string) (DeviceInfo, error)
	// DemoteDevice takes the admin role and key away.
	DemoteDevice(id string) (DeviceInfo, error)
	// CreatePairing creates a code whose device joins profile ("" is the
	// default profile).
	CreatePairing(name, profile string) (PairingInfo, error)
	PairingState(code string) (PairingState, error)
	RevokePairing(code string) error
	// LanPairings lists the requests waiting for approval (ADR-0007).
	LanPairings() []LanPairingRequest
	ApproveLanPairing(id, profile string) (DeviceInfo, error)
	DenyLanPairing(id string) error
	Calls() CallsView
	Stats() Stats
	Logs(after uint64) LogsView
	SelfTest(ctx context.Context) []Check
	Config() map[string]any
}
