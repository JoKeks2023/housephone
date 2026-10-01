# Fertig, wenn

- [x] Admin-Endpunkte nur auf dem privaten Listener; öffentlich und von Loopback `403 home_network_required` (Test `TestAdminOnlyInTheHomeNetwork`, Ende-zu-Ende `TestEndToEndAppAdmin`)
- [x] Ohne Admin-Rolle, ohne `HP2-Admin`, mit Geräteschlüssel statt Admin-Schlüssel, mit fremdem Schlüssel oder Signatur für einen anderen Pfad: `403 admin_required`, Dienst nicht aufgerufen
- [x] Wiederholte Admin-Anfrage: `401`
- [x] Entziehen wirkt mit der nächsten Anfrage
- [x] Watch nie Admin (Befördern, Einrichten, Anfragen)
- [x] Erstes iPhone einer frischen Bridge ist Admin, weitere Geräte und Watches nicht; Einrichtung nur einmal im Fenster, mit gültigem Nachweis
- [x] Jede Admin-Aktion an alle Geräte; Gerätename nur für eigenes Profil und Admins
- [x] Go ↔ Swift: Testvektoren in beiden Implementierungen grün
- [x] `go test -race ./...`, `go vet ./...`, `swift test` grün
- [ ] CI grün (siehe Validierungsbericht)
- [ ] Auf einem echten iPhone mit Face ID im Heimnetz ausprobiert (User)
