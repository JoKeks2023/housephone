package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
)

// stdin is read for confirmations (tests replace it).
var stdin io.Reader = os.Stdin

// lanPairing handles devices pending|approve|deny: pairing requests from
// the home network without a QR code (ADR-0007).
func lanPairing(client *admin.Client, args []string, in io.Reader, out io.Writer) error {
	ctx := context.Background()
	list, err := client.LanPairings(ctx)
	if err != nil {
		return err
	}
	if args[0] == "pending" {
		if len(list) == 0 {
			fmt.Fprintln(out, "Keine Kopplungsanfragen. Housephone auf dem iPhone im Heim-WLAN öffnen und die Bridge antippen.")
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tCODE\tNAME\tMODELL\tADRESSE\tSCHLÜSSEL\tWARTET BIS")
		for _, r := range list {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, admin.GroupSAS(r.SAS), displayName(r.DeviceName),
				displayName(orDash(r.Model)), r.IP, r.KeyFingerprint, r.ExpiresAt.Local().Format("15:04:05"))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(out, "\nFreigeben nur, wenn das iPhone denselben Code zeigt: housephone-bridge devices approve <id>")
		return nil
	}

	fs := flag.NewFlagSet("devices "+args[0], flag.ContinueOnError)
	fs.SetOutput(out)
	code := fs.String("code", "", "Code, den das iPhone zeigt (freigeben ohne Rückfrage)")
	profileID := fs.String("profile", "", "Profil, zu dem das Gerät gehört (ohne Angabe: Rückfrage bei mehreren Profilen, sonst das Standardprofil)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: devices %s [-code 123456] [-profile ID] <id>", args[0])
	}
	req, err := findLanRequest(list, fs.Arg(0))
	if err != nil {
		return err
	}
	if args[0] == "deny" {
		if err := client.DenyLanPairing(ctx, req.ID); err != nil {
			return lanErr(err)
		}
		fmt.Fprintf(out, "Abgelehnt: %s (%s)\n", displayName(req.DeviceName), req.IP)
		return nil
	}

	fmt.Fprintf(out, "Anfrage von %s (%s, %s), Schlüssel %s\nCode auf der Bridge: %s\n", displayName(req.DeviceName),
		displayName(orDash(req.Model)), req.IP, req.KeyFingerprint, admin.GroupSAS(req.SAS))
	reader := bufio.NewReader(in)
	if *code != "" {
		if strings.ReplaceAll(*code, " ", "") != req.SAS {
			return errors.New("der Code stimmt nicht – nicht freigegeben")
		}
	} else {
		fmt.Fprint(out, "Zeigt das iPhone genau diesen Code? [j/N] ")
		answer, _ := reader.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "j" && a != "ja" && a != "y" && a != "yes" {
			fmt.Fprintln(out, "Nicht freigegeben. Die Anfrage wartet weiter; ablehnen mit: devices deny "+req.ID)
			return nil
		}
	}
	chosen := *profileID
	if chosen == "" && *code == "" {
		// ADR-0008: with several profiles the admin picks whose device it is.
		profiles, err := client.Profiles(ctx)
		if err != nil {
			return err
		}
		if chosen, err = askProfile(profiles, reader, out); err != nil {
			return err
		}
	}
	dev, err := client.ApproveLanPairing(ctx, req.ID, chosen)
	if errors.Is(err, admin.ErrUnknownProfile) {
		return fmt.Errorf("unbekanntes Profil %q (housephone-bridge profiles zeigt alle)", chosen)
	}
	if err != nil {
		return lanErr(err)
	}
	fmt.Fprintf(out, "Gekoppelt: %s (%s), Profil %s, ID %s\n", displayName(dev.Name), dev.Platform, displayName(dev.ProfileName), dev.ID)
	return nil
}

// askProfile lets the admin pick a profile by number; with one profile
// there is nothing to ask.
func askProfile(profiles []admin.ProfileInfo, in *bufio.Reader, out io.Writer) (string, error) {
	if len(profiles) < 2 {
		return "", nil
	}
	fmt.Fprintln(out, "Zu welchem Profil gehört das Gerät?")
	for i, p := range profiles {
		fmt.Fprintf(out, "  %d) %s\n", i+1, displayName(p.Name))
	}
	fmt.Fprint(out, "Nummer: ")
	answer, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(answer))
	if err != nil || n < 1 || n > len(profiles) {
		return "", errors.New("keine gültige Auswahl – nicht freigegeben")
	}
	return profiles[n-1].ID, nil
}

// findLanRequest accepts the full ID or a unique prefix of at least four
// characters.
func findLanRequest(list []admin.LanPairingRequest, id string) (admin.LanPairingRequest, error) {
	var found []admin.LanPairingRequest
	for _, r := range list {
		if r.ID == id {
			return r, nil
		}
		if len(id) >= 4 && strings.HasPrefix(r.ID, id) {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return admin.LanPairingRequest{}, fmt.Errorf("keine offene Anfrage %q (abgelaufen? devices pending zeigt alle)", id)
	}
	return admin.LanPairingRequest{}, fmt.Errorf("%q passt auf mehrere Anfragen – mehr Zeichen angeben", id)
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

func lanErr(err error) error {
	if errors.Is(err, admin.ErrNotFound) {
		return errors.New("die Anfrage ist abgelaufen oder wurde schon entschieden")
	}
	return err
}
