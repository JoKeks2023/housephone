# Umfang

Drin:
- `repository.yaml`, `ha-addon/housephone-bridge/` (config.yaml, DOCS, CHANGELOG, README, Icon, Übersetzungen de/en)
- Add-on-Image (Dockerfile-Target `addon`) und Veröffentlichung auf GHCR
- `serve -ha-options`: Optionen → Konfiguration, Secrets nie im Log
- Zugänge im Add-on: Tunnel auf 172.30.32.1, Heimnetz ohne Supervisor-Netz (`excludedNetworks`)
- `internal/dashboard` mit `Gate`-Interface und `SupervisorGate`
- Tests (Go, CI-Nachstellung des Supervisors, Add-on-Linter)
- README-Abschnitt, ADR-0006, Roadmap

Nicht drin:
- Passkey-Gate / Dashboard unter Docker Compose (HPHN-39)
- Selbsttest, Logs und Statistik im Dashboard (bleiben in der TUI)
- Gebündelter Cloudflare Tunnel
- MQTT-Integration (HPHN-29)
