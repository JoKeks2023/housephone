// Command housephone-bridge connects a FRITZ!Box to the Housephone apps.
//
// Usage:
//
//	housephone-bridge [-config config.yaml] serve
//	housephone-bridge serve -ha-options /data/options.json   (Home Assistant add-on)
//	housephone-bridge [-config config.yaml] pair [-name "iPhone"] [-profile ID]
//	housephone-bridge [-config config.yaml] profiles
//	housephone-bridge [-config config.yaml] identity
//	housephone-bridge [-config config.yaml] devices list
//	housephone-bridge [-config config.yaml] devices remove <device-id>
//	housephone-bridge [-config config.yaml] devices move <device-id> <profile-id>
//	housephone-bridge [-config config.yaml] devices promote|demote <device-id>
//	housephone-bridge version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
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
	"github.com/JoKeks2023/housephone/bridge/internal/dashboard"
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
  serve -ha-options PFAD    Bridge als Home-Assistant-Add-on starten (mit Dashboard)
  pair [-name NAME] [-profile ID]
                            Kopplungscode + QR-Code für ein neues Gerät erzeugen
                            und warten, bis es gekoppelt ist (Strg-C: Code ungültig)
  profiles                  Profile (eigene IP-Telefone mit eigener Nummer) anzeigen
  tui                       Admin-Oberfläche (Status, Geräte, Kopplung, Anrufe, Logs, Selbsttest)
  identity                  Fingerabdruck der Bridge anzeigen
  devices list              Gekoppelte Geräte anzeigen
  devices remove <id>       Gerät entfernen (bei laufender Bridge sofort getrennt)
  devices rename <id> NAME  Gerät umbenennen
  devices move <id> PROFIL  Gerät (und seine Uhren) in ein anderes Profil verschieben
  devices pending           Kopplungsanfragen aus dem Heimnetz anzeigen (ohne QR-Code)
  devices approve <id>      Anfrage freigeben, wenn das iPhone denselben Code zeigt
                            (-code 123456 ohne Rückfrage, -profile ID für das Profil)
  devices deny <id>         Anfrage ablehnen
  devices promote <id>      iPhone zum Admin machen (Verwaltung in der App, nur im
                            Heimnetz); es richtet dann innerhalb einer Stunde Face ID ein
  devices demote <id>       Admin-Rechte entziehen
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
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.SetOutput(stderr)
		haOptions := fs.String("ha-options", "", "Home-Assistant-Add-on: Optionen aus dieser Datei statt -config (/data/options.json)")
		if err := fs.Parse(rest[1:]); err != nil {
			return err
		}
		if *haOptions != "" {
			cfg, err := config.LoadHAOptions(*haOptions)
			if err != nil {
				return err
			}
			if err := cfg.ValidateServe(); err != nil {
				return err
			}
			// Only the add-on serves the dashboard: behind ingress, where Home
			// Assistant has authenticated the user.
			gate := dashboard.SupervisorGate{Proxy: netip.MustParseAddr(config.HAIngressProxy)}
			listen := net.JoinHostPort(config.HAGateway, strconv.Itoa(config.HAIngressPort))
			return serve(cfg, stderr, app.WithDashboard(listen, gate, addonSlug(stderr)))
		}
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
		profileID := fs.String("profile", "", "Profil, zu dem das Gerät gehört (ohne Angabe: Standardprofil)")
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
		return pair(ctx, cfg, *name, *profileID, stdout, 500*time.Millisecond)
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
	case "profiles":
		cfg, err := config.Load(*configPath, true)
		if err != nil {
			return err
		}
		return profiles(cfg, stdout)
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

func serve(cfg config.Config, logOut io.Writer, opts ...app.Option) error {
	ring := admin.NewLogRing(1000)
	log := slog.New(ring.Handler(newLogger(cfg.Log.Level, logOut).Handler()))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bridge, err := app.New(ctx, cfg, log, append([]app.Option{app.WithLogRing(ring)}, opts...)...)
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
func pair(ctx context.Context, cfg config.Config, name, profileID string, out io.Writer, poll time.Duration) error {
	prof, ok := cfg.Profiles().Get(profileID)
	if !ok {
		return fmt.Errorf("unbekanntes Profil %q (housephone-bridge profiles zeigt alle)", profileID)
	}
	key, err := app.LoadIdentityKey(cfg.Bridge.DataDir, true)
	if err != nil {
		return err
	}
	// Devices pair only over the private listener in the home network.
	lanURL, err := app.PairingLanURL(ctx, cfg)
	if err != nil {
		return err
	}
	pairing := store.NewPairing(cfg.Bridge.DataDir)
	pc, err := pairing.CreateFor(name, storedProfile(prof.ID), time.Now())
	if err != nil {
		return err
	}
	link := app.PairingLink(cfg.Bridge.PublicURL, lanURL, pc.Code, key.Fingerprint(), cfg.Bridge.Name)
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
	if cfg.Profiles().Multi() {
		fmt.Fprintf(out, "Profil: %s\n", displayName(prof.Name))
	}
	fmt.Fprintf(out, "Link:   %s\n", link)
	fmt.Fprintf(out, "Heimnetz: %s (zum Koppeln muss das Gerät im WLAN oder per Tailscale verbunden sein)\n", lanURL)
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
		profiles := cfg.Profiles()
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tPROFIL\tPLATTFORM\tADMIN\tSCHLÜSSEL\tPUSH\tGEKOPPELT\tÜBER\tZULETZT GESEHEN")
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
			profileName := d.ProfileID() + " (entfernt)"
			if p, ok := profiles.Get(d.ProfileID()); ok {
				profileName = p.Name
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, displayName(d.Name), displayName(profileName), displayName(d.Platform), adminLabel(d, time.Now()), keyInfo, pushInfo, d.CreatedAt.Local().Format("02.01.2006 15:04"), via, lastSeen)
		}
		return tw.Flush()
	}
	client, _ := admin.Dial(cfg.Bridge.DataDir)
	switch args[0] {
	case "pending", "approve", "deny":
		if client == nil {
			return errors.New("die Bridge läuft nicht – Kopplungsanfragen gibt es nur bei laufender Bridge")
		}
		return lanPairing(client, args, stdin, out)
	case "remove":
		if client != nil {
			return removeDeviceLive(client, args[1:], out)
		}
		return removeDevice(reg, store.NewPairing(cfg.Bridge.DataDir), args[1:], out)
	case "move":
		return moveDevice(cfg, reg, client, args[1:], out)
	case "promote", "demote":
		return setAdmin(reg, client, args[0] == "promote", args[1:], out)
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
	return fmt.Errorf("unbekannter devices-Befehl %q (list|remove|rename|move|promote|demote|pending|approve|deny)", args[0])
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

// addonSlug asks the Supervisor for the add-on's slug so the dashboard can
// link to the options. Without it the link is just missing.
func addonSlug(stderr io.Writer) string {
	token := os.Getenv("SUPERVISOR_TOKEN")
	if token == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	slug, err := dashboard.SupervisorAddonSlug(ctx, token)
	if err != nil {
		fmt.Fprintln(stderr, "add-on slug unknown, dashboard without options link:", err)
	}
	return slug
}
