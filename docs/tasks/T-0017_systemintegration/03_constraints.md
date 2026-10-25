# Randbedingungen

- Kein Simulator, keine schweren lokalen Builds: App-Build und Kit-Tests laufen in der CI, der Funktionsnachweis auf dem Gerät.
- Team und Bundle-IDs bleiben in der xcconfig. Die App-Group-ID wird aus `HOUSEPHONE_BUNDLE_PREFIX` abgeleitet, damit das Setup-Skript für fremde Präfixe weiter genügt.
- Entitlements, die Apple auf Antrag vergibt (CarPlay), dürfen das Signieren nicht brechen. Sie sind standardmäßig aus und werden per xcconfig-Schalter eingeschaltet. (Die Local-Push-Extension aus ADR-0005 ist anders gelöst: Sie wird gebaut, aber nicht eingebettet.)
- Erweiterungen (Widgets, Watch-Komplikationen) telefonieren nie selbst. Sie öffnen die App mit einem App Intent, und die App startet den Anruf über CallKit.
- Keine echten Nummern oder Namen in Code, Tests, Screenshots oder Commits.
- Deep Links (`housephone://call`) aus fremden Quellen rufen nie ohne Rückfrage an. Nur Links mit dem Schlüssel aus der App Group (`DeepLinkKey`) und Aufrufe aus der App selbst (Schnellaktionen, App Intents) starten direkt.
