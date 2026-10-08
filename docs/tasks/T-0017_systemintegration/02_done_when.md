# Fertig, wenn

Phase 1:
- „Hey Siri, ruf <Name> mit Housephone an“ startet den Anruf über Housephone. Der Name wird aus Favoriten, iPhone-Kontakten und FRITZ!Box-Telefonbuch aufgelöst.
- In der Kurzbefehle-App gibt es die Aktionen „Favorit anrufen“, „Nummer anrufen“, „Tastenfeld öffnen“ und „Verpasste Anrufe zeigen“. App Shortcuts erscheinen ohne Einrichtung in Spotlight und Siri.
- Langes Drücken auf das App-Icon zeigt bis zu vier Favoriten.
- Die App schreibt bei Änderungen den Schnappschuss in die App Group.
- CI baut die App unsigniert, die Kit-Tests laufen grün.

Phase 2:
- Widgets in der Galerie: Favoriten (klein/mittel, Favoriten wählbar), Anrufe (mittel/groß), Verpasste Anrufe (Sperrbildschirm rund/rechteckig/Zeile). Die kleinen Widgets taugen für StandBy.
- Tippen auf einen Favoriten oder einen Anruf startet den Anruf ohne Rückfrage (Link mit Schlüssel). Tippen auf „Verpasst“ öffnet die gefilterte Anrufliste.
- Ab iOS 18 gibt es im Kontrollzentrum und für den Action-Button „Favorit anrufen“ (konfigurierbar) und „Tastenfeld öffnen“.
- Widgets aktualisieren sich nach jedem Anruf, nach Änderungen an Favoriten und nach dem Ansehen der Anrufliste.
- Die CI prüft, dass die Extension unter `PlugIns/` eingebettet ist, ihre Bundle-ID, die App-Intents-Metadaten und das Privacy-Manifest.

Phase 3 und 4: werden beim Start der jeweiligen Phase hier ergänzt.

Funktionsnachweis: auf echtem iPhone (Siri, Kurzbefehle, Schnellaktionen). Ohne Gerätetest gilt eine Phase als „nicht vollständig verifiziert“.
