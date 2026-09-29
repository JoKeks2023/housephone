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
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

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
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tPLATTFORM\tPUSH\tGEKOPPELT\tZULETZT GESEHEN")
		for _, d := range list {
			pushInfo := "nein"
			if d.PushToken != "" {
				pushInfo = "ja (" + d.PushEnvironment + ")"
			}
			lastSeen := "–"
			if !d.LastSeen.IsZero() {
				lastSeen = d.LastSeen.Local().Format("02.01.2006 15:04")
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, d.Name, d.Platform, pushInfo, d.CreatedAt.Local().Format("02.01.2006 15:04"), lastSeen)
		}
		return tw.Flush()
	}
	if args[0] == "remove" {
		if len(args) != 2 {
			return errors.New("usage: devices remove <device-id>")
		}
		if err := reg.Remove(args[1]); err != nil {
			if errors.Is(err, store.ErrDeviceNotFound) {
				return fmt.Errorf("gerät %s nicht gefunden", args[1])
			}
			return err
		}
		fmt.Fprintf(out, "Gerät %s entfernt. Eine bestehende Verbindung endet beim nächsten Verbindungsaufbau.\n", args[1])
		return nil
	}
	return fmt.Errorf("unbekannter devices-Befehl %q (list|remove)", args[0])
}
