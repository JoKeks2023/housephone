// Command housephone-bridge connects a FRITZ!Box to the Housephone apps.
//
// Usage:
//
//	housephone-bridge [-config config.yaml] serve
//	housephone-bridge [-config config.yaml] pair [-name "iPhone Joris"]
//	housephone-bridge [-config config.yaml] identity
//	housephone-bridge [-config config.yaml] devices list
//	housephone-bridge [-config config.yaml] devices remove <device-id>
//	housephone-bridge version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/mdp/qrterminal/v3"

	"github.com/JoKeks2023/housephone/bridge/internal/admin"
	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/hp2"
	"github.com/JoKeks2023/housephone/bridge/internal/signaling"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
	"github.com/JoKeks2023/housephone/bridge/internal/tui"
	"github.com/JoKeks2023/housephone/bridge/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `housephone-bridge – verbindet die FRITZ!Box mit der Housephone-App

Befehle:
  serve                     Bridge starten
  pair [-name NAME]         Kopplungscode + QR-Code für ein neues Gerät erzeugen
                            und warten, bis es gekoppelt ist (Strg-C: Code ungültig)
  tui                       Admin-Oberfläche (Status, Geräte, Kopplung, Anrufe, Logs, Selbsttest)
  identity                  Fingerabdruck der Bridge anzeigen
  devices list              Gekoppelte Geräte anzeigen
  devices remove <id>       Gerät entfernen (bei laufender Bridge sofort getrennt)
  devices rename <id> NAME  Gerät umbenennen
  version                   Version anzeigen

Globale Optionen:
  -config PFAD              Konfigurationsdatei (Standard: $HOUSEPHONE_CONFIG oder config.yaml)
`)
}

func run(args []string, stdout, stderr io.Writer) error {
	global := flag.NewFlagSet("housephone-bridge", flag.ContinueOnError)
	global.SetOutput(stderr)
	global.Usage = func() { usage(stderr) }
	defaultConfig := os.Getenv(config.EnvConfig)
	if defaultConfig == "" {
		defaultConfig = config.DefaultConfigPath
	}
	configPath := global.String("config", defaultConfig, "configuration file")
	if err := global.Parse(args); err != nil {
		return err
	}
	rest := global.Args()
	if len(rest) == 0 {
		usage(stderr)
		return errors.New("kein Befehl angegeben")
	}

	switch rest[0] {
	case "version":
		fmt.Fprintln(stdout, version.Version)
		return nil
	case "serve":
		cfg, err := config.Load(*configPath, false)
		if err != nil {
			return err
		}
		if err := cfg.ValidateServe(); err != nil {
			return err
		}
		return serve(cfg, stderr)
	case "pair":
		fs := flag.NewFlagSet("pair", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "device name shown in the bridge (optional)")
		if err := fs.Parse(rest[1:]); err != nil {
			return err
		}
		cfg, err := config.Load(*configPath, true)
		if err != nil {
			return err
		}
		if err := cfg.ValidatePair(); err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return pair(ctx, cfg, *name, stdout, 500*time.Millisecond)
	case "identity":
		cfg, err := config.Load(*configPath, true)
		if err != nil {
			return err
		}
		return identity(cfg, stdout)
	case "devices":
		cfg, err := config.Load(*configPath, true)
		if err != nil {
			return err
		}
		return devices(cfg, rest[1:], stdout)
	case "tui":
		cfg, err := config.Load(*configPath, true)
		if err != nil {
			return err
		}
		client, err := admin.Dial(cfg.Bridge.DataDir)
		if err != nil {
			return fmt.Errorf("%w – erst `serve` starten (im Container läuft sie automatisch)", err)
		}
		if !isTerminal(stdout) {
			return tui.Snapshot(client, stdout)
		}
		return tui.Run(client)
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	}
	usage(stderr)
	return fmt.Errorf("unbekannter Befehl %q", rest[0])
}

func newLogger(level string, w io.Writer) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: l}))
}

func serve(cfg config.Config, logOut io.Writer) error {
	ring := admin.NewLogRing(1000)
	log := slog.New(ring.Handler(newLogger(cfg.Log.Level, logOut).Handler()))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bridge, err := app.New(ctx, cfg, log, app.WithLogRing(ring))
	if err != nil {
		return err
	}
	return bridge.Run(ctx)
}

// errPairingAborted and errPairingExpired end the pair command without a
// new device.
var (
	errPairingAborted = errors.New("abgebrochen – der Code ist jetzt ungültig")
	errPairingExpired = errors.New("der Code ist abgelaufen, es wurde kein Gerät gekoppelt")
)

// pair creates a one-time code, shows it as QR code, link and grouped
// text, and waits until a device used it (and reports which one) or the
// code expired. Cancelling ctx (Ctrl-C) revokes the code.
func pair(ctx context.Context, cfg config.Config, name string, out io.Writer, poll time.Duration) error {
	key, err := app.LoadIdentityKey(cfg.Bridge.DataDir, true)
	if err != nil {
		return err
	}
	pairing := store.NewPairing(cfg.Bridge.DataDir)
	pc, err := pairing.Create(name, time.Now())
	if err != nil {
		return err
	}
	link := app.PairingLink(cfg.Bridge.PublicURL, pc.Code, key.Fingerprint(), cfg.Bridge.Name)
	fmt.Fprintln(out, "Öffne die Housephone-App und scanne diesen QR-Code (die Kopplung erscheint beim ersten Start und nach dem Entkoppeln):")
	fmt.Fprintln(out)
	qrterminal.GenerateWithConfig(link, qrterminal.Config{
		Level:          qrterminal.L,
		Writer:         out,
		HalfBlocks:     true,
		BlackChar:      qrterminal.BLACK_BLACK,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
		WhiteChar:      qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		QuietZone:      2,
	})
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Code:   %s\n", hp2.GroupCode(pc.Code))
	fmt.Fprintf(out, "Bridge: %s\n", key.Fingerprint())
	fmt.Fprintf(out, "Link:   %s\n", link)
	fmt.Fprintf(out, "Gültig: bis %s (einmalig)\n", pc.ExpiresAt.Local().Format("15:04:05"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Warte auf das Gerät … (Strg-C bricht ab und macht den Code ungültig)")

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		used, found, err := pairing.UsedBy(pc.Code, time.Now())
		if err != nil {
			return err
		}
		if found {
			return reportPaired(cfg, used, out)
		}
		if !time.Now().Before(pc.ExpiresAt) {
			return errPairingExpired
		}
		select {
		case <-ctx.Done():
			revoked, err := pairing.Revoke(pc.Code)
			if err != nil {
				return err
			}
			if !revoked {
				// Used in the meantime: report the device instead.
				if used, found, err := pairing.UsedBy(pc.Code, time.Now()); err == nil && found {
					return reportPaired(cfg, used, out)
				}
			}
			return errPairingAborted
		case <-ticker.C:
		}
	}
}

// reportPaired shows which device used the code, so an unexpected pairing
// is noticed right away.
func reportPaired(cfg config.Config, used store.UsedCode, out io.Writer) error {
	dev, err := store.NewDevices(cfg.Bridge.DataDir).Get(used.DeviceID)
	if err != nil {
		return fmt.Errorf("code von Gerät %s benutzt, das nicht mehr gekoppelt ist: %w", used.DeviceID, err)
	}
	model := dev.Model
	if model == "" {
		model = "–"
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Gekoppelt:")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  Gerät:\t%s\n", displayName(dev.Name))
	fmt.Fprintf(tw, "  Modell:\t%s\n", displayName(model))
	fmt.Fprintf(tw, "  Plattform:\t%s\n", displayName(dev.Platform))
	fmt.Fprintf(tw, "  Schlüssel:\t%s\n", hp2.KeyFingerprint(dev.PublicKey))
	fmt.Fprintf(tw, "  ID:\t%s\n", dev.ID)
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(out, "Warst du das nicht? Sofort entfernen: housephone-bridge devices remove", dev.ID)
	return nil
}

// identity shows the bridge's fingerprint, which the apps pin when
// pairing (the fp parameter of the pairing link).
func identity(cfg config.Config, out io.Writer) error {
	key, err := app.LoadIdentityKey(cfg.Bridge.DataDir, false)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Fingerabdruck der Bridge (steht als fp= in jedem Kopplungslink):")
	fmt.Fprintf(out, "  %s\n", key.Fingerprint())
	fmt.Fprintf(out, "  %s\n", hp2.FingerprintHex(key.PublicKey()))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Schlüsseldatei: %s\n", store.IdentityKeyPath(cfg.Bridge.DataDir))
	fmt.Fprintln(out, "Sichere sie zusammen mit dem Datenverzeichnis. Geht sie verloren, müssen alle Geräte neu gekoppelt werden.")
	return nil
}

func devices(cfg config.Config, args []string, out io.Writer) error {
	reg := store.NewDevices(cfg.Bridge.DataDir)
	if len(args) == 0 || args[0] == "list" {
		list, err := reg.List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintln(out, "Keine Geräte gekoppelt. Neues Gerät: housephone-bridge pair")
			return nil
		}
		names := make(map[string]string, len(list))
		for _, d := range list {
			names[d.ID] = displayName(d.Name)
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tPLATTFORM\tSCHLÜSSEL\tPUSH\tGEKOPPELT\tÜBER\tZULETZT GESEHEN")
		for _, d := range list {
			pushInfo := "nein"
			if d.PushToken != "" {
				pushInfo = "ja (" + d.PushEnvironment + ")"
			}
			lastSeen := "–"
			if !d.LastSeen.IsZero() {
				lastSeen = d.LastSeen.Local().Format("02.01.2006 15:04")
			}
			via := "–"
			if d.PairedBy != "" {
				via = names[d.PairedBy]
				if via == "" {
					via = "entferntes Gerät " + d.PairedBy
				}
			}
			keyInfo := hp2.KeyFingerprint(d.PublicKey)
			if d.PublicKey == "" {
				keyInfo = "fehlt (neu koppeln)"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, displayName(d.Name), displayName(d.Platform), keyInfo, pushInfo, d.CreatedAt.Local().Format("02.01.2006 15:04"), via, lastSeen)
		}
		return tw.Flush()
	}
	client, _ := admin.Dial(cfg.Bridge.DataDir)
	switch args[0] {
	case "remove":
		if client != nil {
			return removeDeviceLive(client, args[1:], out)
		}
		return removeDevice(reg, store.NewPairing(cfg.Bridge.DataDir), args[1:], out)
	case "rename":
		if len(args) != 3 {
			return errors.New("usage: devices rename <device-id> NAME")
		}
		if client != nil {
			d, err := client.RenameDevice(context.Background(), args[1], args[2])
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Umbenannt: %s (%s)\n", d.ID, displayName(d.Name))
			return nil
		}
		d, err := reg.Update(args[1], func(d *store.Device) { d.Name = signaling.SanitizeName(args[2]) })
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Umbenannt: %s (%s)\n", d.ID, displayName(d.Name))
		return nil
	}
	return fmt.Errorf("unbekannter devices-Befehl %q (list|remove|rename)", args[0])
}

// removeDeviceLive removes a device through the running bridge, which
// cuts off its connection and calls immediately.
func removeDeviceLive(client *admin.Client, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("devices remove", flag.ContinueOnError)
	fs.SetOutput(out)
	keep := fs.Bool("keep-companions", false, "über dieses Gerät gekoppelte Uhren behalten")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: devices remove [-keep-companions] <device-id>")
	}
	res, err := client.RemoveDevice(context.Background(), fs.Arg(0), *keep)
	if errors.Is(err, admin.ErrNotFound) {
		return fmt.Errorf("gerät %s nicht gefunden", fs.Arg(0))
	}
	if err != nil {
		return err
	}
	for _, d := range res.Removed {
		fmt.Fprintf(out, "Entfernt und getrennt: %s (%s)\n", d.ID, displayName(d.Name))
	}
	for _, d := range res.Kept {
		fmt.Fprintf(out, "Behalten: %s (%s)\n", d.ID, displayName(d.Name))
	}
	return nil
}

// isTerminal reports whether out is an interactive terminal; without one,
// `tui` prints a text snapshot instead.
func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// displayName prints stored names safely: names with characters that are
// not printable (older entries could contain terminal escapes) are quoted.
func displayName(s string) string {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return strconv.Quote(s)
		}
	}
	return s
}

// removeDevice removes a device and, unless -keep-companions is given, the
// watches paired through it: they were paired with its credentials, so a
// lost or compromised iPhone must not leave its watches behind. Open
// companion codes of the device are dropped as well. Connected devices are
// cut off by the running bridge within its revalidation interval (10 s).
func removeDevice(reg *store.Devices, pairing *store.Pairing, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("devices remove", flag.ContinueOnError)
	fs.SetOutput(out)
	keep := fs.Bool("keep-companions", false, "über dieses Gerät gekoppelte Uhren behalten")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: devices remove [-keep-companions] <device-id>")
	}
	id := fs.Arg(0)
	dev, err := reg.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrDeviceNotFound) {
			return fmt.Errorf("gerät %s nicht gefunden", id)
		}
		return err
	}
	companions, err := reg.Companions(id)
	if err != nil {
		return err
	}
	if err := reg.Remove(id); err != nil && !errors.Is(err, store.ErrDeviceNotFound) {
		return err
	}
	if err := pairing.RemoveByParent(id); err != nil {
		return err
	}
	fmt.Fprintf(out, "Gerät %s (%s) entfernt.\n", dev.ID, displayName(dev.Name))
	for _, c := range companions {
		if *keep {
			fmt.Fprintf(out, "Behalten: %s (%s), gekoppelt über dieses Gerät.\n", c.ID, displayName(c.Name))
			continue
		}
		if err := reg.Remove(c.ID); err != nil && !errors.Is(err, store.ErrDeviceNotFound) {
			return err
		}
		fmt.Fprintf(out, "Ebenfalls entfernt: %s (%s), gekoppelt über dieses Gerät.\n", c.ID, displayName(c.Name))
	}
	fmt.Fprintln(out, "Offene Verbindungen trennt die laufende Bridge innerhalb von 10 s; laufende Anrufe dieser Geräte werden beendet.")
	return nil
}
