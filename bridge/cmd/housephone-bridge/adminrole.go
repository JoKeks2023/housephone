package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/signaling"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// adminLabel is the ADMIN column of devices list (ADR-0009).
func adminLabel(d store.Device, now time.Time) string {
	switch {
	case !d.Admin || !d.CanBeAdmin():
		return "–"
	case d.AdminEnrollOpen(now):
		return "ja (Face ID einrichten bis " + d.AdminEnrollUntil.Local().Format("15:04") + ")"
	case d.AdminKey == "":
		return "ja (Face ID fehlt: erneut promote)"
	default:
		return "ja"
	}
}

// setAdmin promotes or demotes a device: through the running bridge, which
// tells the device and all others, or directly in the store.
func setAdmin(reg *store.Devices, client *admin.Client, promote bool, args []string, out io.Writer) error {
	if len(args) != 1 {
		if promote {
			return errors.New("usage: devices promote <device-id>")
		}
		return errors.New("usage: devices demote <device-id>")
	}
	id := args[0]
	if client != nil {
		var (
			d   admin.DeviceInfo
			err error
		)
		if promote {
			d, err = client.PromoteDevice(context.Background(), id)
		} else {
			d, err = client.DemoteDevice(context.Background(), id)
		}
		switch {
		case errors.Is(err, admin.ErrNotFound):
			return fmt.Errorf("gerät %s nicht gefunden", id)
		case errors.Is(err, admin.ErrNotAllowed):
			return errors.New("nur ein iPhone kann Admin sein, keine Apple Watch")
		case err != nil:
			return err
		}
		return reportAdmin(out, promote, displayName(d.Name), d.ID, d.AdminEnrollUntil)
	}
	dev, err := reg.Get(id)
	if err != nil {
		return fmt.Errorf("gerät %s nicht gefunden", id)
	}
	if promote && !dev.CanBeAdmin() {
		return errors.New("nur ein iPhone kann Admin sein, keine Apple Watch")
	}
	until := time.Now().Add(signaling.AdminEnrollWindow).UTC()
	dev, err = reg.Update(id, func(d *store.Device) {
		d.Admin = promote
		if promote {
			d.AdminEnrollUntil = until
		} else {
			d.AdminKey = ""
			d.AdminEnrollUntil = time.Time{}
		}
	})
	if err != nil {
		return err
	}
	if !promote {
		until = time.Time{}
	}
	return reportAdmin(out, promote, displayName(dev.Name), dev.ID, until)
}

func reportAdmin(out io.Writer, promote bool, name, id string, until time.Time) error {
	if !promote {
		fmt.Fprintf(out, "Kein Admin mehr: %s (%s)\n", name, id)
		return nil
	}
	fmt.Fprintf(out, "Admin: %s (%s)\n", name, id)
	if !until.IsZero() {
		fmt.Fprintf(out, "Öffne auf dem iPhone im Heim-WLAN Einstellungen → Verwaltung und richte Face ID ein (bis %s).\n", until.Local().Format("15:04"))
	}
	return nil
}
