# Validierung

- `go test -race ./internal/calls/` grün (dreimal), `go vet ./...` sauber.
- App-Build und HousephoneKit-Tests: CI.
- Offen: Anruf auf dem iPhone über den Tunnel ohne Portfreigabe (Ton in beide Richtungen), eingehend und ausgehend.
