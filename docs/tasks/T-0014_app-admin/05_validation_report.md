# Validierung

## Lokal

- `go vet ./...` und `go test -race ./...` in `bridge/`: alle 22 Pakete grün.
  - Neu: `signaling/adminapi_test.go` (erstes iPhone Admin, einmalige Einrichtung, falscher Nachweis, Geräteschlüssel als Admin-Schlüssel, Fenster abgelaufen, fehlende/falsche/fremde Admin-Signatur, Signatur für anderen Pfad, Replay `401`, Entziehen wirkt sofort, Watch nie Admin, nur privater Listener und nicht von Loopback, ohne Backend keine Admin-API, `admin.action` mit Ziel nur fürs eigene Profil und Admins, `admin.role`).
  - `app/e2e_appadmin_test.go`: laufende Bridge, Einrichtung über den LAN-Listener, öffentlich `403`, Umbenennen und Befördern aus der „App“, Meldungen an beide Geräte, Entziehen über den Admin-Socket.
  - `hp2/admin_test.go`: Testvektoren; dashboard-, tui- und admin-Tests für Befördern/Entziehen.
- `swift test` in `ios/Packages/HousephoneKit`: 235 Tests grün, darunter `AdminTests` (Vektoren mit CryptoKit, beide Signaturen am Stub-Bridge geprüft, Pfad mit Query, LAN-Adresse, Fehlerabbildung) und die Fixture-Round-Trips mit `admin.action`, `admin.role`, `welcome.admin`.
- String-Kataloge: alter Inhalt und Reihenfolge per Skript unverändert, 89 + 1 Einträge neu.

## CI

- Bridge 36853398355, Home-Assistant-Add-on 36853398369: grün (Commit f066bb7c).
- iOS 36853398393: rot, `LocalAuthentication`-APIs auf watchOS nicht verfügbar; behoben in 86415f99 (Store nur außerhalb von watchOS).
- iOS 36853636570: grün (Commit 86415f99), ohne Warnungen in den neuen Dateien.

## Nicht geprüft

- Die Oberfläche auf einem Gerät (kein Simulator, kein lokaler Xcode-Build).
- Face ID mit echtem Secure-Enclave-Schlüssel, insbesondere dass mehrere Lese-Anfragen mit einem `LAContext` nur einmal fragen und dass ein neu eingerichtetes Face ID den Schlüssel unbrauchbar macht.
- Verwaltung gegen die laufende Bridge des Users im Heimnetz und über Tailscale.
