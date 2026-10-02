package admin

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// CheckInput is what the self-test looks at; the app fills it from the
// running components so the rules stay testable.
type CheckInput struct {
	Registrar     string // as configured
	RegistrarIP   string // resolved at start
	SIPRegistered bool
	SIPUser       string
	// Profiles beyond the default one get a check each (ADR-0008).
	Profiles []ProfileInfo

	FritzBoxConfigured bool
	FritzBoxFeatures   []string

	// PushMode is config.PushMode* ("apns", "relay", "off").
	PushMode      string
	PushRelay     string
	APNsKeyLoaded bool
	LastPush      *PushInfo

	PublicIP       string
	PublicIPSource string
	PublicURL      string
	Listen         string
	MediaPort      int

	DevicesTotal    int
	DevicesWithPush int
	// DevicesWithoutPushKey have a push token but no push key (apps older
	// than ADR-0010); the relay cannot wake them.
	DevicesWithoutPushKey int
}

// RunChecks turns the input into traffic lights with a hint each.
func RunChecks(in CheckInput) []Check {
	var out []Check
	add := func(name string, st CheckState, detail, hint string) {
		out = append(out, Check{Name: name, State: st, Detail: detail, Hint: hint})
	}

	// FRITZ!Box registration.
	if in.SIPRegistered {
		add("FRITZ!Box-Anmeldung", CheckOK, fmt.Sprintf("angemeldet als %s an %s", in.SIPUser, in.RegistrarIP), "")
	} else {
		target := in.RegistrarIP
		if in.Registrar != "" && in.Registrar != in.RegistrarIP {
			target += " (" + in.Registrar + ")"
		}
		add("FRITZ!Box-Anmeldung", CheckFail, "nicht angemeldet an "+target,
			"IP-Telefon in der FRITZ!Box prüfen: Benutzername/Kennwort, und dass „sip.registrar“ die IP der FRITZ!Box ist.")
	}

	// Further profiles: their own IP phones, and own numbers for the call
	// list.
	for i, p := range in.Profiles {
		name := "Profil „" + p.Name + "“"
		if i > 0 {
			if p.Registered {
				add(name, CheckOK, "angemeldet als "+p.SIPUser, "")
			} else {
				add(name, CheckFail, "nicht angemeldet als "+p.SIPUser,
					"Eigenes IP-Telefon für dieses Profil in der FRITZ!Box anlegen und Benutzer/Kennwort in lines[] prüfen.")
			}
		}
		if len(in.Profiles) > 1 && in.FritzBoxConfigured && !p.HistoryAllowed {
			add(name+": Anrufliste", CheckWarn, "keine eigenen Nummern eingetragen",
				"numbers für dieses Profil setzen, sonst sehen seine Geräte keine Anrufliste.")
		}
	}

	// Telephone book / call list.
	switch {
	case !in.FritzBoxConfigured:
		add("Telefonbuch & Anrufliste", CheckWarn, "nicht eingerichtet (optional)", "FRITZ!Box-Benutzer mit Recht „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“ anlegen und fritzbox.username setzen.")
	case len(in.FritzBoxFeatures) == 2:
		add("Telefonbuch & Anrufliste", CheckOK, "beide abrufbar", "")
	default:
		add("Telefonbuch & Anrufliste", CheckFail, "Abruf fehlgeschlagen", "Log prüfen (Tab 5, Suche „fritzbox“): Kennwort, Benutzerrechte, „Zugriff für Anwendungen zulassen“.")
	}

	// Push.
	switch {
	case in.PushMode == "relay":
		switch {
		case in.LastPush != nil && !in.LastPush.OK:
			add("Push (Relay)", CheckFail, "letzter Push fehlgeschlagen: "+in.LastPush.Error, "Ist "+in.PushRelay+" erreichbar? Bei 403 passt das App-Bundle nicht zum Relay; dann eigenes Relay oder eigenen APNs-Key nutzen.")
		case in.LastPush != nil:
			add("Push (Relay)", CheckOK, "über "+in.PushRelay+", letzter Push erfolgreich", "")
		default:
			add("Push (Relay)", CheckOK, "über "+in.PushRelay+" (noch kein Push gesendet)", "")
		}
		if in.DevicesWithoutPushKey > 0 {
			add("Push-Schlüssel", CheckWarn, fmt.Sprintf("%d Gerät(e) ohne Push-Schlüssel", in.DevicesWithoutPushKey), "App aktualisieren und einmal öffnen; über das Relay gehen nur verschlüsselte Pushes.")
		}
	case in.PushMode != "apns":
		add("Push", CheckFail, "nicht eingerichtet", "Push-Relay (apns.relay) eintragen oder einen eigenen APNs-Key – sonst klingeln Geräte nur bei offener App.")
	case !in.APNsKeyLoaded:
		add("Push (APNs)", CheckFail, "Key nicht lesbar", "Pfad und Rechte der .p8-Datei prüfen.")
	case in.LastPush != nil && !in.LastPush.OK:
		add("Push (APNs)", CheckFail, "letzter Push fehlgeschlagen: "+in.LastPush.Error, "InvalidProviderToken → Key-ID/Team-ID; BadDeviceToken → App einmal öffnen (Debug vs. TestFlight).")
	case in.LastPush != nil:
		add("Push (APNs)", CheckOK, "Key geladen, letzter Push erfolgreich", "")
	default:
		add("Push (APNs)", CheckOK, "Key geladen (noch kein Push gesendet)", "")
	}
	if in.DevicesTotal > 0 && in.DevicesWithPush == 0 {
		add("Push-Token", CheckWarn, "kein Gerät hat ein Push-Token gemeldet", "App auf dem iPhone einmal öffnen, danach meldet sie ihr VoIP-Token.")
	}

	// Public reachability.
	if in.PublicIP == "" {
		add("Öffentliche IP", CheckFail, "unbekannt", "media.publicIp setzen oder UPnP-Statusinformationen in der FRITZ!Box aktivieren.")
	} else {
		add("Öffentliche IP", CheckOK, fmt.Sprintf("%s (Quelle: %s)", in.PublicIP, in.PublicIPSource), "")
	}
	add("Medienport", CheckWarn, fmt.Sprintf("UDP %d lokal offen", in.MediaPort),
		fmt.Sprintf("Von hier nicht messbar: Portfreigabe UDP %d → dieser Server in der FRITZ!Box prüfen.", in.MediaPort))

	// Public URL.
	u, err := url.Parse(in.PublicURL)
	switch {
	case in.PublicURL == "" || err != nil:
		add("Öffentliche Adresse", CheckFail, "bridge.publicUrl fehlt", "Hostnamen des Cloudflare Tunnels eintragen, z. B. phone.example.com.")
	case u.Scheme != "wss":
		add("Öffentliche Adresse", CheckWarn, in.PublicURL+" ist unverschlüsselt", "Für unterwegs wss:// über den Cloudflare Tunnel verwenden.")
	case strings.Contains(u.Host, "example"):
		add("Öffentliche Adresse", CheckFail, "noch der Beispielwert: "+in.PublicURL, "Eigenen Hostnamen des Tunnels eintragen.")
	default:
		add("Öffentliche Adresse", CheckOK, in.PublicURL, "")
	}

	// Devices.
	if in.DevicesTotal == 0 {
		add("Geräte", CheckWarn, "noch kein Gerät gekoppelt", "Tab 3 (Kopplung) öffnen und den QR-Code mit dem iPhone scannen.")
	} else {
		add("Geräte", CheckOK, fmt.Sprintf("%d gekoppelt", in.DevicesTotal), "")
	}
	return out
}

// Worst returns the most severe state of checks.
func Worst(checks []Check) CheckState {
	states := make([]CheckState, 0, len(checks))
	for _, c := range checks {
		states = append(states, c.State)
	}
	switch {
	case slices.Contains(states, CheckFail):
		return CheckFail
	case slices.Contains(states, CheckWarn):
		return CheckWarn
	}
	return CheckOK
}
