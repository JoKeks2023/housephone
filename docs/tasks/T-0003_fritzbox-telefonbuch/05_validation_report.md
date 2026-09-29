# Validierungsbericht T-0003

**Stand:** 2026-09-29

## Belegt

| Prüfung | Ergebnis |
|---|---|
| Bridge `go vet`, `go test -race ./...` | grün, lokal und in der CI (PR #12) |
| Test-FRITZ!Box (TLS, Digest, AVM-XML nach `x_contactSCPD` v41) | Ausgabe **identisch** mit `docs/protocol/fixtures/http/*.json`; per absichtlich verfälschter Fixture gegengeprüft |
| Randfälle | Sommer- und Winterzeit inkl. Umstellungstage; alle Anruftypen und Ports (Fax, Anrufbeantworter); unterdrückte Nummer; falsches Passwort und UPnP-Fehler → 503; 20 gleichzeitige Anfragen → 1 Abruf |
| E2E | Echte Bridge gegen Fake-SIP und Fake-TR-064: Listen gleich den Fixtures, ETag/304, `welcome.features`; Anruf ohne Anzeigenamen → Push und `call.incoming` mit „Oma“ |
| Kit `swift test` | 109 Tests in 16 Suites grün |
| iPhone- und Watch-Quellen | `swiftc -typecheck` mit Swift 6 strikt sauber; CI-Build der App mit Watch grün |
| Echte FRITZ!Box, ohne Anmeldung | `GetSecurityPort` → 49443; `X_AVM-DE_OnTel` unter `/upnp/control/x_contact` in der `tr64desc.xml` vorhanden |

## Nicht belegt (braucht den FRITZ!Box-Benutzer, siehe `bridge/README.md` Abschnitt 8)

- Angemeldete TR-064-Aktionen und Downloads an der echten FRITZ!Box (FRITZ!OS 8.25): echtes XML, `sid`-Downloads, Verhalten bei fehlendem Recht
- UI auf echten Geräten

**Status: Code fertig und automatisiert geprüft, funktional nicht verifiziert.**
