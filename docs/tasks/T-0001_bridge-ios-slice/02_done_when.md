# Fertig, wenn

## Code (in dieser Session prüfbar)

- [ ] `go vet ./...` und `go test ./...` im Ordner `bridge/` sind grün (lokal und in der CI)
- [ ] `swift test` für `HousephoneKit` ist grün (lokal und in der CI)
- [ ] Die iOS-App baut in der CI unsigniert für `generic/platform=iOS`
- [ ] Die Protokoll-Nachrichten stimmen in Go und Swift überein (Round-Trip-Tests gegen dieselben JSON-Beispieldateien)

## Funktion (nur mit Nutzer, echtem iPhone, FRITZ!Box und Server prüfbar)

- [ ] Bridge ist an der FRITZ!Box registriert (`/v1/health` → `sipRegistered: true`)
- [ ] iPhone gekoppelt
- [ ] Festnetzanruf bei gesperrtem iPhone im Mobilfunk (WLAN aus) → CallKit klingelt → Annehmen → Gespräch in beide Richtungen
- [ ] Ausgehender Anruf aus der App → Freiton → Gespräch → Auflegen
- [ ] Anruf taucht in der iPhone-Anrufliste auf; Rückruf von dort startet Housephone
