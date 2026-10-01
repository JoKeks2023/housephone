# Umfang

## Drin

- Konfiguration: `sip` = Standardprofil, `profile` (Name, Nummern, Telefonbücher), `lines[]` für weitere Profile; Validierung; Home-Assistant-Optionen `profile_name`, `profile_numbers`, `lines`.
- Bridge: eine SIP-Registrierung pro Profil auf eigenem Port; Anrufe, Push, Status, ausgehende Leitung, Anrufliste, Telefonbücher und Anrufernamen pro Profil; Profilprüfung bei jeder Anruf-Nachricht.
- Geräte: Profil in `devices.json`, beim Koppeln gewählt (QR, Heimnetz-Freigabe), Watch erbt vom iPhone, Verschieben trennt die Verbindung (4004).
- Verwaltung: Admin-API, CLI (`pair -profile`, `profiles`, `devices move`, `devices approve -profile`), TUI, Dashboard, Selbsttest, Statistik pro Profil.
- Protokoll: `welcome.profile`, Close-Code 4004, Abschnitt „Profile“ in `signaling-v2.md`, Fixture `welcome.json`.
- App: Profil und eigene Nummer in den Einstellungen und der Übersichtskarte.
- Doku: ADR-0008, README, Add-on-Doku und -Changelog (0.2.0), `config.example.yaml`, Roadmap.

## Nicht drin

- Profilwahl bzw. Freigabe am Admin-iPhone (HPHN-37).
- IP-Telefone automatisch per TR-064 in der FRITZ!Box anlegen (für die Bridge).
- Profile im Direktmodus der App (ohne Bridge).
