# Fertig, wenn

- [x] Ein Anruf auf Leitung B klingelt und pusht nur bei Geräten von B (Unit-Test `calls`, Ende-zu-Ende mit nachgebauter FRITZ!Box)
- [x] Ein Gerät von A kann einen Anruf von B weder annehmen noch anhängen noch abfragen, auch nicht nach dem Ende
- [x] Ausgehend wählt B über das eigene IP-Telefon (Ende-zu-Ende: `From` ist B's SIP-Benutzer)
- [x] Anrufliste mit mehreren Profilen nach eigenen Nummern gefiltert, ohne Nummern keine Liste; mit einem Profil unverändert
- [x] Telefonbücher und Anrufernamen pro Profil eingrenzbar
- [x] Bestehende Konfigurationen und Geräte funktionieren unverändert (Standardprofil)
- [x] Koppeln ins Profil per QR und Heimnetz-Freigabe; Watch erbt das Profil; Verschieben trennt mit 4004 und verbindet ins neue Profil
- [x] CLI, TUI, Dashboard und Admin-API mit Tests
- [x] `go test -race ./...`, `go vet ./...`, `swift test` grün
- [ ] CI grün
- [ ] Mit zwei echten IP-Telefonen an der FRITZ!Box getestet (User)
