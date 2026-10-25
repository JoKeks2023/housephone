# Zukunftsplan

**Stand:** 2026-09-29

## Erledigt

- Bridge und iPhone-App (T-0001), Watch-App (T-0002), FRITZ!Box-Telefonbuch und -Anrufliste (T-0003)
- Sicherheitsbefunde behoben
- Anmeldung v2 mit Secure Enclave und Ende-zu-Ende-Verschlüsselung (T-0004), Design-Durchgang (T-0007)
- Watch koppelt sich automatisch
- Bridge-Image auf GHCR
- Admin-TUI (`./housephone tui`)
- Vortest gegen die echte FRITZ!Box: eingehend und ausgehend, Ton in beide Richtungen

## Als Nächstes

| # | Aufgabe | Kurz |
|:-:|---|---|
| 1 | **Live-Test auf dem Server** | Einrichtung nach README, Selbsttest in der TUI |
| 2 | **Zwei Zugänge** | Öffentlich nur Telefonie; Kopplung und Verwaltung nur im Heimnetz oder über Tailscale; zu Hause Direktverbindung zur Bridge ohne Tunnel |
| 3 | **Verwaltung in der App** | Nur im Heimnetz, Admin-Rolle, Face ID (HPHN-37, ADR-0009); Freigabe von Heimnetz-Kopplungen mit Profilwahl am Admin-iPhone |
| 4 | **Koppeln im Heimnetz ohne QR** | Bonjour, Bestätigungscode, Freigabe in TUI, CLI und HA-Dashboard (HPHN-41, ADR-0007); Freigabe am Admin-iPhone folgt mit Nr. 3 |
| 5 | **Profile** | Mehrere Nutzer mit eigener Festnetznummer, je ein IP-Telefon an der FRITZ!Box (HPHN-42, ADR-0008); Profilwahl am Admin-iPhone folgt mit Nr. 3 |
| 6 | **Home Assistant** | MQTT-Discovery (nur ausgehend), Sensoren, Ereignis „Anruf eingehend“ |
| 7 | **Home-Assistant-Add-on** | Bridge als Add-on mit Dashboard per Ingress (HPHN-51, ADR-0006) |
| 8 | *Optional:* Web-UI ohne Home Assistant | Dasselbe Dashboard über den Heimnetz-Zugang mit Passkey-Gate (HPHN-39), Zitadel optional |
| 9 | **Systemintegration** (T-0017) | Siri, Kurzbefehle, Schnellaktionen; Widgets und Kontrollzentrum; Watch-Komplikationen und Favoriten; CarPlay (Entitlement auf Antrag) |

## Später

- Mac-App
- Veröffentlichung (siehe `docs/veroeffentlichung.md`): Push-Relay, eigenes Repo und Lizenz, Demo-Zugang für App Review, einfachere Einrichtung

## Ideen

- Halten, Makeln, Weiterleiten (SIP REFER)
- TURN-Fallback für restriktive Mobilfunknetze
- Anrufbeantworter-Nachrichten abspielen
- G.722 auf der Watch
- Liquid-Glass-App-Icon in mehreren Ebenen
- Schreibbare Einstellungen in der TUI
