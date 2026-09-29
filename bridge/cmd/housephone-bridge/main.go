// Command housephone-bridge connects a FRITZ!Box to the Housephone apps.
//
// Usage:
//
//	housephone-bridge [-config config.yaml] serve
//	housephone-bridge [-config config.yaml] pair [-name "iPhone Joris"]
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

	"github.com/JoKeks2023/housephone/bridge/internal/app"
	"github.com/JoKeks2023/housephone/bridge/internal/config"
	"github.com/JoKeks2023/housephone/bridge/internal/store"
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
  devices list              Gekoppelte Geräte anzeigen
  devices remove <id>       Gerät entfernen
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
		return pair(cfg, *name, stdout)
	case "devices":
		cfg, err := config.Load(*configPath, true)
		if err != nil {
			return err
		}
		return devices(cfg, rest[1:], stdout)
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
	log := newLogger(cfg.Log.Level, logOut)
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bridge, err := app.New(ctx, cfg, log)
	if err != nil {
		return err
	}
	return bridge.Run(ctx)
}

func pair(cfg config.Config, name string, out io.Writer) error {
	pc, err := store.NewPairing(cfg.Bridge.DataDir).Create(name, time.Now())
	if err != nil {
		return err
	}
	link := app.PairingLink(cfg.Bridge.PublicURL, pc.Code, cfg.Bridge.Name)
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
	fmt.Fprintf(out, "Code:   %s\n", pc.Code)
	fmt.Fprintf(out, "Link:   %s\n", link)
	fmt.Fprintf(out, "Gültig: bis %s (einmalig)\n", pc.ExpiresAt.Local().Format("15:04:05"))
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
		fmt.Fprintln(tw, "ID\tNAME\tPLATTFORM\tPUSH\tGEKOPPELT\tÜBER\tZULETZT GESEHEN")
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
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, displayName(d.Name), displayName(d.Platform), pushInfo, d.CreatedAt.Local().Format("02.01.2006 15:04"), via, lastSeen)
		}
		return tw.Flush()
	}
	if args[0] == "remove" {
		return removeDevice(reg, store.NewPairing(cfg.Bridge.DataDir), args[1:], out)
	}
	return fmt.Errorf("unbekannter devices-Befehl %q (list|remove)", args[0])
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
