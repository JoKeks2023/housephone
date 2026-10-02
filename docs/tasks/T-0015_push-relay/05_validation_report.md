# Validierung

| Prüfung | Ergebnis |
|---|---|
| `go vet ./...`, `go test ./...` (bridge) | grün |
| `TestEndToEndPushThroughRelay`: Bridge ohne Key + nachgebautes Relay + echte FRITZ!Box-Attrappe; Push versiegelt, mit Geräteschlüssel geöffnet, Nummer/Name/Call-ID stimmen | grün |
| Relay-Tests (privates Repo): Klartext, fremdes Topic, Großschreibung, zu groß, Rate-Limit pro Token und Adresse (X-Forwarded-For), 410/502, APNs-Header | grün (lokal) |
| `TestVectors` (Go) und `SealedPushTests` (Swift, 9 Tests) gegen `push-vectors.json` | grün (Swift: `swift test` in HousephoneKit, 244 Tests) |
| `TestPushKeyIsStoredAndValidated`: Schlüssel aus `hello` und `PUT /v1/device`, ungültige ignoriert | grün |
| App-Targets (iPhone, Watch) bauen | nur CI |

**Nicht verifiziert:**
- Push über das echte APNs aus dem Relay (kein Deploy, keine Domain).
- Klingeln auf echtem iPhone/Watch mit versiegeltem Push; Schlüsselbund-Zugriff im Sperrbildschirm.
- Relay hinter dem Cloudflare Tunnel auf dem VPS (Erreichbarkeit, X-Forwarded-For).
