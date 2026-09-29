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
| 3 | **Verwaltung in der App** | Nur im Heimnetz, Admin-Rolle, Face ID |
| 4 | **Koppeln im Heimnetz ohne QR** | Bonjour, Bestätigungscode, Freigabe durch ein Admin-Gerät |
| 5 | **Profile** | Mehrere Nutzer mit eigener Festnetznummer, je ein IP-Telefon an der FRITZ!Box |
| 6 | **Home Assistant** | MQTT-Discovery (nur ausgehend), Sensoren, Ereignis „Anruf eingehend“ |
| 7 | *Optional:* Web-UI | Über Tailscale-HTTPS mit Passkey, Zitadel optional |

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
