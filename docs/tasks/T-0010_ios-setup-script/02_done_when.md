# Fertig, wenn

- [x] Ohne `Local.xcconfig` lösen sich Team und Bundle-IDs zu den bisherigen Werten auf (`xcodebuild -showBuildSettings`)
- [x] Mit `Local.xcconfig` gelten deren Werte (`org.example` → `org.example.housephone`)
- [x] Skript läuft in einem Pseudo-Terminal durch: Prüfung, Team aus dem Schlüsselbund, Präfix, Zusammenfassung, Speichern, Fertig-Screen; Pfeiltasten, „Andere …“ mit leerer Eingabe zurück; Terminal wird wiederhergestellt
- [x] CI: ungültige Eingaben werden abgelehnt, App wird mit fremdem Team/Präfix gebaut, Bundle-IDs und Companion-ID der Watch stimmen im Build
- [ ] Doppelklick im Finder auf dem Mac des Users (User)
