# Validierungsbericht T-0002

**Stand:** 2026-09-29

## Belegt

| Prüfung | Ergebnis |
|---|---|
| Bridge `go vet`, `go test -race ./...` | grün (CI Run 36552902253) |
| Bridge E2E Watch: eingehend bis „connected“ | PCMA in beide Richtungen, gleichmäßiger 20-ms-Takt nach einem TCP-Schub (fortlaufende Seq, Timestamp-Schritt 160) |
| Bridge E2E Watch: ausgehend | nur PCMA angeboten, Ton, BYE |
| Bridge E2E Watch: Klingeln ohne WebSocket, Anrufer legt auf | `GET /v1/calls/{id}` → `ended/remote_cancelled` |
| Call-Manager | iPhone + Watch klingeln; wer annimmt, bestimmt den Codec (G.722 bzw. PCMA); der andere bekommt `answered_elsewhere` |
| Kit `swift test` | 91 Tests in 12 Suites grün (G.711-Referenzwerte, Jitter-Puffer, HTTPS-Stub, Fixtures) |
| Watch-Quellen | `swiftc -typecheck` gegen watchOS-SDK, Swift 6 strikt: 0 Fehler, 0 Warnungen |
| iOS-App mit eingebetteter Watch-App | unsignierter CI-Build grün (Run 36552902287); Watch-App unter `PlugIns/` mit korrekter Info.plist |

## Nicht belegt (braucht echte Uhr, iPhone, Bridge)

- VoIP-Push und CallKit auf watchOS; ob die Uhr beim Klingeln HTTPS abfragen darf
- Echounterdrückung und Mikrofonformat auf der Uhr, Jitter im echten Netz, TCP-Latenz über Cloudflare Tunnel im Mobilfunk
- WatchConnectivity (Kopplung und Entkoppeln), Installation der eingebetteten Watch-App auf dem Gerät

**Status: Code fertig und automatisiert geprüft, funktional nicht verifiziert.**
