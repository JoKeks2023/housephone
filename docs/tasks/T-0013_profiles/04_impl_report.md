# Umsetzung

## Bridge

| Bereich | Datei(en) | Was |
|---|---|---|
| Modell | `internal/profile/profile.go` | `Profile`, `Set` (Standard zuerst, Nullwert = ein Profil), `HistoryAllowed`, `OwnsNumber` (Ziffern ohne führende Nullen, Endstück ≥ 5) |
| Konfiguration | `internal/config/config.go`, `haoptions.go` | `profile`, `lines[]`, `passwordFile`, Port `sip.bindPort + 1 + i`, Validierung (max. 8, IDs, doppelte Benutzer/Ports, Nummern, Telefonbuch-IDs); HA-Optionen |
| Speicher | `internal/store/devices.go`, `pairing.go` | `Device.Profile` (leer = Standard), `PairingCode.Profile`, `CreateFor` |
| SIP | `internal/app/app.go` | ein `sipleg.Leg` pro weiterem Profil, gleiche Bind-Adresse, eigener Port, eigener Log-Kontext |
| Anrufe | `internal/calls/manager.go`, `call.go`, `events.go`, `interfaces.go` | `SetLine`, `HandleIncomingFor`, Filter der Geräte pro Profil, Profilprüfung in `withCall` (auch Tombstones), Wählen über eigene Leitung, `BroadcastProfileStatus`, `SIPRegisteredFor`, `Event.Profile`, `DeviceConn.ProfileID` |
| Signalisierung | `internal/signaling/profiles.go`, `session.go`, `server.go`, `directory.go`, `lanpair.go` | `welcome.profile`, Features pro Profil, Anrufliste/Telefonbuch pro Profil, Watch erbt Profil, `device.paired` nur im Profil, Verschieben → 4004, `ApproveLanPairingFor` |
| FRITZ!Box | `internal/fritzbox/directory.go`, `calllist.go` | `Phonebook(books)`, `History(own)`, `CallerNameIn`, `HistoryCall.OwnNumber` (nie gesendet) |
| Verwaltung | `internal/admin/*`, `internal/app/admin.go` | `ProfileInfo`, `Profiles()`, `MoveDevice`, Profil bei Code und Freigabe, Statistik `byProfile`, Selbsttest pro Profil, Line-Kennwörter in `Config()` geschwärzt |
| CLI | `cmd/housephone-bridge/profiles.go`, `main.go`, `lanpair.go` | `profiles`, `devices move`, `pair -profile`, `devices approve -profile` bzw. Rückfrage |
| TUI | `internal/tui/tui.go` | Profile in der Übersicht, Spalte „Profil“, Taste `p`, Profilwahl beim Koppeln/Freigeben, Statistik pro Profil |
| Dashboard | `internal/dashboard/*` | Profilkarte, Auswahl beim Koppeln/Freigeben, Verschieben (CSRF wie alle POSTs) |
| Test-FRITZ!Box | `internal/fakefritz/box.go` | weitere IP-Telefone (`AddUser`, `CallPhone`) |

## App

- `HousephoneKit`: `Welcome.profile`, `BridgeProfile` (`isWorthShowing`: nicht bei einem Profil ohne Nummer).
- Einstellungen: „Profil“ und „Eigene Nummer“ im Bridge-Abschnitt; Übersichtskarte mit Zeile „Profil“. Zwei neue Texte de/en im Xcode-Format.

## Protokoll und Doku

- `docs/protocol/signaling-v2.md` (Abschnitt „Profile (v2.2)“, Close-Code 4004), `fixtures/welcome.json` mit `profile`.
- ADR-0008, README (Schritt „Mehrere Personen“, Alltag), Add-on `DOCS.md`, `CHANGELOG.md` 0.2.0, `config.yaml` 0.2.0, `config.example.yaml`, Roadmap.

## Nebenbei

- `internal/tui/tui_test.go`: maskierte Beispielnummer „…563“ durch „…567“ ersetzt (fiktiv wie die übrigen).
