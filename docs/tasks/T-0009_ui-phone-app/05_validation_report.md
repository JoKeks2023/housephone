# Validierungsbericht

| Prüfung | Ergebnis | Beleg |
|---|---|---|
| App-Build unsigniert (iOS + Watch + Local-Push-Extension) | grün, keine Swift-Warnungen | CI-Lauf 36831715422 |
| HousephoneKit-Tests | grün (Kit unverändert) | CI-Lauf 36831715422 |
| Reformat der Kataloge ohne Inhaltsänderung | JSON-gleich zu `HEAD` | Commit `66259a4c`, Prüfung per JSON-Vergleich aller vier Kataloge |
| Formatierer = Xcode-Format | byte-gleich mit Katalogen, die Xcode lokal geschrieben hat | Vergleich gegen die vom User gebauten Dateien |
| Neue Texte Deutsch/Englisch | 36 neue Schlüssel mit englischer Übersetzung | `Localizable.xcstrings` |

## Nicht verifiziert

- Die Oberfläche selbst: kein Simulator, kein Gerät. Layout, Abstände, Farben im hellen und dunklen Modus und Dynamic Type sind ungesehen.
- Ob iOS beim eingehenden Anruf das Kontaktposter zeigt.
- Wie sich die Vorschläge im Tastenfeld bei sehr großen Adressbüchern anfühlen: Sie werden bei jedem Tastendruck über alle Kontakte berechnet.
