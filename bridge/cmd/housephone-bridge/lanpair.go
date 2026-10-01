package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: devices %s [-code 123456] <id>", args[0])
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
	if *code != "" {
		if strings.ReplaceAll(*code, " ", "") != req.SAS {
			return errors.New("der Code stimmt nicht – nicht freigegeben")
		}
	} else {
		fmt.Fprint(out, "Zeigt das iPhone genau diesen Code? [j/N] ")
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "j" && a != "ja" && a != "y" && a != "yes" {
			fmt.Fprintln(out, "Nicht freigegeben. Die Anfrage wartet weiter; ablehnen mit: devices deny "+req.ID)
			return nil
		}
	}
	dev, err := client.ApproveLanPairing(ctx, req.ID)
	if err != nil {
		return lanErr(err)
	}
	fmt.Fprintf(out, "Gekoppelt: %s (%s), ID %s\n", displayName(dev.Name), dev.Platform, dev.ID)
	return nil
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
