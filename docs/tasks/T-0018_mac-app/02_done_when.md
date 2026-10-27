# Fertig, wenn

- Phase 1: `HousephoneMac` baut in CI (`xcodebuild -scheme HousephoneMac -destination 'generic/platform=macOS'`), die Kopplungslink-Prüfung funktioniert beim Start der App.
- Phase 2: Der Mac erscheint in der Geräteliste der Bridge und bleibt verbunden (Reconnect nach Ruhezustand).
- Phase 3: Echter Testanruf in beide Richtungen mit Ton, an echter Hardware beobachtet.
- Phase 4: Telefonbuch, Anrufliste und `tel:`-Link funktionieren.
- Keine Phase gilt als verifiziert ohne Beobachtung auf einem echten Mac.
