# Umfang

**Drin:**

- Bridge: Bonjour-Ankündigung (`internal/bonjour`, abschaltbar `bridge.bonjour` / Add-on-Option `bonjour`), drei Endpunkte auf dem privaten Listener (`/v1/pair/lan…`), Anfragen im Speicher mit Ablauf und Grenzen, Admin-API `/v1/lan-pairings`.
- Freigabe: TUI (Tab „Kopplung“, `a`/`d`), CLI `devices pending|approve|deny`, HA-Dashboard.
- Krypto: Commitment, signiertes Angebot, Beweis, SAS und versiegelte Freigabe in Go (`internal/hp2`) und Swift (`HousephoneKit/LanPairing.swift`), Testvektoren, HTTP-Fixtures.
- iOS: `NWBrowser`, Onboarding mit gefundenen Bridges, Kopplungs-Sheet mit Code und Warten auf Freigabe, Adresseingabe für Tailscale, `NSBonjourServices`, Texte de/en.
- Doku: ADR-0007, `signaling-v2.md`, README, Add-on-Doku, Roadmap.

**Nicht drin:**

- Freigabe am Admin-iPhone mit Face ID (HPHN-37), Profilwahl (HPHN-42).
- Erkennen einer schon gekoppelten Bridge in der App über das `fp`-Präfix (es gibt noch keinen Einstieg außerhalb des Onboardings).
- Kopplung der Watch ohne iPhone.
