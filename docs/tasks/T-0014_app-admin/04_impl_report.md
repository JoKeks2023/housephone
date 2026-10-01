# Umsetzung

## Bridge

- `internal/hp2/admin.go`: `HP2-Admin`-Header, `AdminMessage` (Label `HP2-ADMIN`, sonst Felder wie `HP2-AUTH`), `AdminEnrollMessage`, Prüf- und Client-Funktionen; `hp2.Client.AdminKey`.
- `internal/store/devices.go`: `admin`, `adminKey`, `adminEnrollUntil`; `CanBeAdmin` (nur `ios`), `AdminEnrollOpen`, `AddPromotingFirst` (erstes iPhone einer leeren Bridge wird Admin, unter Dateilock).
- `internal/signaling/adminapi.go`: Routen nur auf dem privaten Mux, öffentlich `403 home_network_required`; `adminAuthed` = HP2 + Rolle + Admin-Signatur bei jeder Anfrage; `POST /v1/admin/enroll` mit Fenster und Besitznachweis; `AnnounceAdminAction` (Ziel nur für eigenes Profil und Admins), `SendAdminRole`, `welcome.admin`.
- `internal/app/admin.go`: `adminService` mit Akteur; `PromoteDevice`/`DemoteDevice`; jede ändernde Aktion meldet `admin.action`.
- `internal/protocol`: `admin.action`, `admin.role`, `welcome.admin`, Fehlercodes `admin_required`, `admin_enroll_closed`, `not_allowed`, `not_found`, Bodies.
- CLI `devices promote|demote` (`cmd/housephone-bridge/adminrole.go`), Spalte ADMIN in `devices list`; TUI `a`/`A`; Dashboard-Knöpfe; Admin-Socket `POST /v1/devices/{id}/promote|demote`.
- Testvektoren `docs/protocol/fixtures/crypto/admin-vectors.json` aus `cmd/_adminvectors`.

## iOS

- HousephoneKit: `HP2Admin.swift` (Nachrichten, `HP2Exchange.adminSignature`, `AdminEnrollment`), `AdminKey.swift` (`KeychainAdminKeyStore`: Secure Enclave, `.privateKeyUsage + .biometryCurrentSet`, `WhenPasscodeSetThisDeviceOnly`; `InMemoryAdminKeyStore` für Tests), `BridgeAdminClient.swift` (Modelle und alle Endpunkte, immer über `credentials.lanURL`), `SignalingMessage` (`AdminRole`, `AdminAction`, neue Fälle und Codes).
- App: `Bridge/AdminCenter.swift` (Lesen teilt eine Face-ID-Prüfung für 2 min, jede Änderung fragt neu; Fehlertexte), `Features/Admin/*` (Abschnitt in den Einstellungen, Verwaltung, Gerät, Freigabe mit SAS und Profil, QR-Einladung), `Features/Security/AdminActionBanner.swift`, `BridgeConnection` (`adminRole`, `lastAdminAction`), `NSFaceIDUsageDescription`.
- 89 neue Texte de/en in `Localizable.xcstrings`, 1 in `InfoPlist.xcstrings`; bestehende Einträge und Reihenfolge unverändert (per Skript geprüft).

## Entscheidungen (Details in ADR-0009)

- Bestehende Installationen: niemand wird automatisch Admin; einmal `devices promote`.
- Neuer Admin-Schlüssel nur nach erneuter Beförderung (Schutz gegen Telefon + Code ohne Gesicht).
- Die Watch ist nie Admin, auch wenn `devices.json` anderes sagt.
- Admin-Aktionen vom Server (CLI/TUI/Dashboard) werden ebenfalls gemeldet, `actor` leer.
