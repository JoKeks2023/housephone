# Validierung

| Prüfung | Ergebnis | Beleg |
|---|---|---|
| `go vet ./...`, `go test -race ./...` lokal | grün | alle Pakete ok, u. a. `internal/dashboard`, `internal/config`, `internal/app` |
| Dashboard-Tests | grün | nur 172.30.32.2 (403 für LAN, Loopback, andere Add-ons, auch mit gefälschtem `X-Forwarded-For`), `X-Ingress-Path` geprüft (fremde Werte → `/`), CSRF (ohne/falsch/cross-site → 403, kein Zustand geändert), Koppeln, Warten, Widerrufen, Umbenennen, Entfernen mit Uhren |
| E2E im Prozess | grün | ohne `WithDashboard` kein Listener; mit Dashboard echter Kopplungscode im Store |
| Optionen | grün | Mapping, Defaults, Validierung; kaputtes JSON gibt keine Werte im Fehler aus |
| CI „Bridge“ (Run 36721484497) | grün | Add-on-Image gebaut und wie vom Supervisor gestartet: Tunnel-Health auf 172.30.32.1:8080, Dashboard von 172.30.32.2 mit Ingress-Pfad, 403 von 172.30.32.9 und vom Host, Heimnetz-Zugang 403 aus dem Supervisor-Netz, SIP-Kennwort nicht im Log |
| CI „Home Assistant add-on“ (Run 36720858775) | grün | `frenck/action-addon-linter`, Ingress-Port-Abgleich |

## Nicht verifiziert

- Installation in einer echten Home-Assistant-Instanz (Add-on-Store, Ingress-Panel, AppArmor, Sicherung).
- Veröffentlichung des Add-on-Images (`publish-addon` läuft erst auf `master`).
- Aussehen des Dashboards im Browser (nur HTML-Inhalte getestet).
- Zusammenspiel mit dem Cloudflared-Add-on und einer echten FRITZ!Box im Add-on.
