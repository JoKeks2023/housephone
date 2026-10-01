# Validierung

## Lokal

- `go vet ./...` und `go test -race ./...` (Bridge): grün.
- Neue Tests:
  - `internal/profile`: Nummernvergleich, Profilmenge.
  - `internal/config/profiles_test.go`: YAML mit `lines`, `passwordFile`, Ports, Validierungsfehler, max. 8 Profile, HA-Optionen.
  - `internal/calls/profiles_test.go`: Leitung B klingelt/pusht nur B; A kann B's Anruf weder anhängen noch annehmen noch beantworten noch abfragen (auch Tombstone); Wählen über eigene Leitung; abgemeldete oder fehlende Leitung → `sip_unavailable`; Status nur ans eigene Profil.
  - `internal/signaling/profiles_test.go`: Koppeln ins Profil, `welcome.profile`, Features ohne Anrufliste für Profil ohne Nummern, Watch erbt Profil, Telefonbuch/Anrufliste mit Profilfilter, entferntes Profil → 403, Verschieben → 4004 und neues `welcome`, `device.paired` nur im Profil.
  - `internal/fritzbox/profiles_test.go`: Telefonbuch pro Profil (ein Abruf), Anrufername nur aus eigenen Büchern, Anrufliste gefiltert ohne `OwnNumber` in der Antwort, `Called`/`Caller` als eigene Seite.
  - `internal/admin`, `internal/tui`, `internal/dashboard`, `cmd/housephone-bridge`: API, Auswahl, Verschieben, Rückfrage beim Freigeben.
  - `internal/app/e2e_profiles_test.go` (Ende-zu-Ende mit nachgebauter FRITZ!Box, zwei IP-Telefone): Anruf auf B klingelt nur B, ein Push; A bekommt `not_found`; B wählt mit `From: 621`; Verschieben über den Admin-Socket.
- `swift test` (HousephoneKit): 227 Tests grün, neu `ProfileTests` und Fixture-Round-Trip mit `profile`.

## CI

- Siehe Push von `feat/profiles` (Bridge, App-Build, Add-on-Linter).

## Nicht verifiziert

- Zwei echte IP-Telefone an der FRITZ!Box: Ob die Box beide Registrierungen von derselben IP mit verschiedenen Ports sauber auseinanderhält, ist nur mit der nachgebauten Box getestet.
- Der Abgleich der eigenen Nummern mit der echten FRITZ!Box-Anrufliste (Schreibweise von `Called`/`Caller`).
- Anzeige in der App (kein Simulator); Dashboard und TUI nur über Tests, nicht im Browser bzw. Terminal angesehen.
- Home-Assistant-Add-on mit `lines`-Option in einer echten Installation.
