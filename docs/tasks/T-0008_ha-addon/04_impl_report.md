# Umsetzung

| Teil | Datei(en) |
|---|---|
| Optionen → Konfiguration | `bridge/internal/config/haoptions.go` (+ Test) |
| Ausgeschlossene Netze | `config.Bridge.ExcludedNetworks`, `signaling.PrivateHandler(trusted, excluded...)` |
| Status um Heimnetz-Zugang ergänzt | `admin.Status.PrivateListen`, `LanURL` |
| Dashboard | `bridge/internal/dashboard/` (Seiten, `static/`, `templates/`, i18n de/en) |
| Zugriffsschicht | `dashboard.Gate` (`Guard`, `BasePath`); `SupervisorGate` |
| Einbindung | `app.WithDashboard`, `serve -ha-options` in `cmd/housephone-bridge` |
| Image | `bridge/Dockerfile` Target `addon`; `.github/workflows/bridge-image.yml` Job `publish-addon` |
| Add-on | `repository.yaml`, `ha-addon/housephone-bridge/*` |
| CI | `bridge.yml` (Supervisor-Nachstellung), `ha-addon.yml` (Linter, Ingress-Port-Abgleich) |
| Doku | ADR-0006, README (Abschnitt Add-on), `docs/roadmap.md` |

## Naht für HPHN-39

Die Seiten sprechen nur mit `dashboard.Service` (erfüllt vom Admin-Service) und fragen `Gate.BasePath` nach dem Pfad. Für Docker Compose kommt ein Passkey-Gate hinzu (Anmeldung, Sitzungs-Cookie, `BasePath` = ""), das am Heimnetz-Zugang oder einem eigenen Listener hängt. `WithDashboard(listen, gate)` nimmt es schon heute an; nur `serve` ohne `-ha-options` setzt es noch nicht.

## Abweichungen

- Kein `logo.png` (nur `icon.png`); der Add-on-Store zeigt dann das Icon.
- Selbsttest, Logs und Statistik gibt es nicht im Dashboard, nur in der TUI.
