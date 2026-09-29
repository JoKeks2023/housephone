# Fertig, wenn

## Code

- [ ] Bridge: `go vet`, `go test -race` grün.
  - Neue End-to-End-Tests: ein Test-Gerät mit `websocket-pcma` nimmt einen eingehenden Anruf an → PCMA in beide Richtungen; ausgehender Anruf vom Watch-Gerät.
- [ ] Kit: `swift test` grün, mit Tests für G.711, Rahmenformat, Jitter-Puffer, neue Nachrichten und Fixtures.
- [ ] CI: iOS-App inkl. eingebetteter Watch-App baut unsigniert.

## Funktion (nur mit Nutzer prüfbar)

- [ ] Watch über das iPhone gekoppelt
- [ ] Festnetzanruf → iPhone und Watch klingeln → an der Watch annehmen → Gespräch über Watch-Lautsprecher/-Mikro in beide Richtungen → iPhone hört auf zu klingeln
- [ ] Ausgehender Anruf von der Watch
