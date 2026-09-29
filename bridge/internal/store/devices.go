package store

import (
	"errors"
	"path/filepath"
	"sort"
	"time"
)

// ErrDeviceNotFound is returned when a device ID is unknown.
var ErrDeviceNotFound = errors.New("device not found")

// Device is a paired phone or watch.
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Model    string `json:"model,omitempty"`
	// PublicKey is the device's P-256 signing key (base64url X9.63,
	// signaling v2). The bridge never stores a device secret.
	PublicKey       string `json:"publicKey"`
	PushToken       string `json:"pushToken,omitempty"`
	PushEnvironment string `json:"pushEnvironment,omitempty"`
	// PushTopic overrides the configured APNs topic (e.g. the watch app's).
	PushTopic string `json:"pushTopic,omitempty"`
	// MediaCapabilities as reported by the device; empty means ["webrtc"].
	MediaCapabilities []string `json:"mediaCapabilities,omitempty"`
	// PairedBy is the device that requested the companion pairing code
	// this device paired with (the iPhone of a watch). Empty for devices
	// paired with a code from the pair command.
	PairedBy  string    `json:"pairedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen,omitzero"`
}

// MediaWebSocketPCMA is the media capability of devices without WebRTC (the
// watch). It mirrors protocol.MediaWebSocketPCMA; store must not import
// protocol.
const MediaWebSocketPCMA = "websocket-pcma"

// mediaWebRTC mirrors protocol.MediaWebRTC.
const mediaWebRTC = "webrtc"

// UsesWebSocketAudio reports whether calls reach this device over the
// websocket-pcma media path. Devices that also support WebRTC (or report
// nothing) use WebRTC.
func (d Device) UsesWebSocketAudio() bool {
	ws, webrtc := false, len(d.MediaCapabilities) == 0
	for _, c := range d.MediaCapabilities {
		switch c {
		case MediaWebSocketPCMA:
			ws = true
		case mediaWebRTC:
			webrtc = true
		}
	}
	return ws && !webrtc
}

type devicesFile struct {
	Devices []Device `json:"devices"`
}

// Devices is the file-backed device registry (devices.json).
type Devices struct {
	path string
}

// NewDevices returns the registry stored in dataDir.
func NewDevices(dataDir string) *Devices {
	return &Devices{path: filepath.Join(dataDir, "devices.json")}
}

func (d *Devices) load() (devicesFile, error) {
	var f devicesFile
	err := readJSON(d.path, &f)
	return f, err
}

// List returns all devices ordered by creation time.
func (d *Devices) List() ([]Device, error) {
	var out []Device
	err := withLock(d.path, func() error {
		f, err := d.load()
		out = f.Devices
		return err
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, err
}

// Get returns the device with the given ID.
func (d *Devices) Get(id string) (Device, error) {
	var dev Device
	err := withLock(d.path, func() error {
		f, err := d.load()
		if err != nil {
			return err
		}
		for _, candidate := range f.Devices {
			if candidate.ID == id {
				dev = candidate
				return nil
			}
		}
		return ErrDeviceNotFound
	})
	return dev, err
}

// Add stores a new device.
func (d *Devices) Add(dev Device) error {
	return withLock(d.path, func() error {
		f, err := d.load()
		if err != nil {
			return err
		}
		for _, existing := range f.Devices {
			if existing.ID == dev.ID {
				return errors.New("device already exists")
			}
		}
		f.Devices = append(f.Devices, dev)
		return writeJSON(d.path, f)
	})
}

// Update applies fn to the device and stores the result.
func (d *Devices) Update(id string, fn func(*Device)) (Device, error) {
	var updated Device
	err := withLock(d.path, func() error {
		f, err := d.load()
		if err != nil {
			return err
		}
		for i := range f.Devices {
			if f.Devices[i].ID == id {
				fn(&f.Devices[i])
				updated = f.Devices[i]
				return writeJSON(d.path, f)
			}
		}
		return ErrDeviceNotFound
	})
	return updated, err
}

// Remove deletes a device. It returns ErrDeviceNotFound if it did not exist.
func (d *Devices) Remove(id string) error {
	return withLock(d.path, func() error {
		f, err := d.load()
		if err != nil {
			return err
		}
		kept := f.Devices[:0]
		found := false
		for _, dev := range f.Devices {
			if dev.ID == id {
				found = true
				continue
			}
			kept = append(kept, dev)
		}
		if !found {
			return ErrDeviceNotFound
		}
		f.Devices = kept
		return writeJSON(d.path, f)
	})
}

// Companions returns the devices paired through a code that parentID
// requested (pair.companion.request).
func (d *Devices) Companions(parentID string) ([]Device, error) {
	all, err := d.List()
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, dev := range all {
		if dev.PairedBy == parentID {
			out = append(out, dev)
		}
	}
	return out, nil
}

// ClearPushToken removes the push token if it still equals token, so a token
// refreshed in the meantime is not lost.
func (d *Devices) ClearPushToken(id, token string) error {
	_, err := d.Update(id, func(dev *Device) {
		if dev.PushToken == token {
			dev.PushToken = ""
		}
	})
	return err
}
