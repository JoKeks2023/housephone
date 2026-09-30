# Fertig, wenn

- [x] `go vet ./...` und `go test -race ./...` grün, mit Tests für Optionen, Gate, CSRF, Koppeln, Umbenennen, Entfernen
- [x] Ohne `-ha-options` gibt es keinen Dashboard-Listener (E2E-Test)
- [x] CI baut das Add-on-Image und startet es wie der Supervisor: Tunnel-Health auf 172.30.32.1, Dashboard nur von 172.30.32.2, 403 von anderen Adressen, Heimnetz-Zugang weist das Supervisor-Netz ab, SIP-Kennwort nicht im Log
- [x] Add-on-Linter grün
- [ ] Installation in einer echten Home-Assistant-Instanz (User)
