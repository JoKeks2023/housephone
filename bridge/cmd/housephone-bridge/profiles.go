package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/profile"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
)

// Household profiles (ADR-0008): every profile is its own IP phone at the
// FRITZ!Box; every device belongs to exactly one.

// storedProfile is the profile as stored with a device or code: the
// default profile is stored as "" (like devices from before profiles).
func storedProfile(id string) string {
	if id == profile.DefaultID {
		return ""
	}
	return id
}

// profiles lists the configured profiles; with a running bridge also their
// registration and devices.
func profiles(cfg config.Config, out io.Writer) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if client, _ := admin.Dial(cfg.Bridge.DataDir); client != nil {
		list, err := client.Profiles(context.Background())
		if err != nil {
			return err
		}
		fmt.Fprintln(tw, "ID\tNAME\tIP-TELEFON\tANGEMELDET\tNUMMERN\tGERÄTE")
		for _, p := range list {
			registered := "nein"
			if p.Registered {
				registered = "ja"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d (%d online)\n", p.ID, displayName(p.Name), displayName(p.SIPUser), registered, numbers(p.Numbers, p.HistoryAllowed), p.Devices, p.DevicesOnline)
		}
		return tw.Flush()
	}
	set := cfg.Profiles()
	fmt.Fprintln(tw, "ID\tNAME\tIP-TELEFON\tNUMMERN")
	for _, p := range set.List() {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.ID, displayName(p.Name), displayName(p.SIPUser), numbers(p.Numbers, set.HistoryAllowed(p)))
	}
	return tw.Flush()
}

func numbers(list []string, historyAllowed bool) string {
	if len(list) == 0 {
		if !historyAllowed {
			return "– (keine Anrufliste)"
		}
		return "–"
	}
	return strings.Join(list, ", ")
}

// moveDevice moves a device and the watches paired through it to another
// profile. A running bridge reconnects them right away.
func moveDevice(cfg config.Config, reg *store.Devices, client *admin.Client, args []string, out io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: devices move <device-id> <profil-id>")
	}
	id, target := args[0], args[1]
	if client != nil {
		res, err := client.MoveDevice(context.Background(), id, target)
		switch {
		case errors.Is(err, admin.ErrUnknownProfile):
			return fmt.Errorf("unbekanntes Profil %q (housephone-bridge profiles zeigt alle)", target)
		case errors.Is(err, admin.ErrNotFound):
			return fmt.Errorf("kein Gerät %q", id)
		case err != nil:
			return err
		}
		for _, d := range res.Moved {
			fmt.Fprintf(out, "Verschoben: %s (%s) → %s\n", d.ID, displayName(d.Name), displayName(d.ProfileName))
		}
		return nil
	}
	p, ok := cfg.Profiles().Get(target)
	if !ok {
		return fmt.Errorf("unbekanntes Profil %q (housephone-bridge profiles zeigt alle)", target)
	}
	dev, err := reg.Get(id)
	if err != nil {
		return err
	}
	companions, err := reg.Companions(id)
	if err != nil {
		return err
	}
	for _, d := range append([]store.Device{dev}, companions...) {
		if _, err := reg.Update(d.ID, func(d *store.Device) { d.Profile = storedProfile(p.ID) }); err != nil {
			return err
		}
		fmt.Fprintf(out, "Verschoben: %s (%s) → %s\n", d.ID, displayName(d.Name), displayName(p.Name))
	}
	return nil
}
