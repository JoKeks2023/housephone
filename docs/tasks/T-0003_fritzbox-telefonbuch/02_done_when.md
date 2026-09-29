# Fertig, wenn

## Code

- [ ] Bridge: `go vet`, `go test -race` grün.
  - Fake-TR-064-Server mit Digest-Auth und realistischen AVM-XML-Dateien (nach `x_contactSCPD` v41) → exakt die HTTP-Fixtures.
  - Zeitzonen-Umrechnung inkl. Sommer-/Winterzeit.
  - 503-Fälle, ETag/304, Anrufername im Push.
- [ ] Kit: `swift test` grün, dekodiert die HTTP-Fixtures, Nummernabgleich getestet.
- [ ] CI: iOS-App mit Watch baut.

## Funktion (mit Nutzer)

- [ ] FRITZ!Box-Benutzer angelegt → `welcome.features` enthält beide Funktionen
- [ ] Kontakte → FRITZ!Box zeigt das Telefonbuch; ein Tipp ruft an
- [ ] Anrufe → FRITZ!Box zeigt auch Anrufe am Schnurlostelefon
- [ ] Eingehender Anruf zeigt den Namen aus dem FRITZ!Box-Telefonbuch
