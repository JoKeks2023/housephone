# Validierung

| Prüfung | Ergebnis | Beleg |
|---|---|---|
| `go vet ./...`, `go test -race ./...` (Bridge) | grün, alle Pakete | lokal, CI „Bridge“ Lauf 36835753769 |
| Signaling-Ablauf (Freigabe, Ablehnung, Ablauf, falsches Commitment, falscher Beweis, Absenderbindung, Grenzen, Startprüfung, öffentlicher Listener) | 9 Tests grün, auch mit `-count=2` | `internal/signaling/lanpair_test.go` |
| Krypto-Vektoren Go | grün; gefälschtes Angebot abgelehnt | `internal/hp2/lanpair_test.go` |
| Bonjour: TXT ohne Geheimnisse, Instanzname, Adresswahl, PTR/SRV/TXT-Antworten | grün (ohne Multicast) | `internal/bonjour/bonjour_test.go` |
| Admin-API, CLI, TUI, Dashboard | grün (Fakes) | `admin_test.go`, `cmd/housephone-bridge/lanpair_test.go`, `tui_test.go`, `dashboard_test.go` |
| HTTP-Fixtures `pair-lan.*.json` | Round-Trip grün | `internal/protocol/messages_test.go` |
| `swift test` HousephoneKit | 207 Tests grün, davon 10 neu (Vektoren, Client-Ablauf) | lokal, CI „iOS“ Lauf 36835753817 |
| App-Build (iPhone + Watch, unsigniert) | grün, keine Swift-Warnungen | CI „iOS“ Lauf 36835753817 |
| Add-on-Linter | grün | CI „Home Assistant add-on“ Lauf 36835753762 |

**Nicht geprüft:**

- Bonjour im echten WLAN (Multicast), `NWBrowser` und Auflösung auf IPv4 auf einem iPhone.
- Oberfläche (Onboarding mit gefundener Bridge, Code-Sheet, Adresseingabe) auf Gerät oder Simulator.
- Bonjour im HA-Add-on neben dem mDNS-Dienst von Home Assistant.
- Ende-zu-Ende gegen die laufende Bridge des Users.
