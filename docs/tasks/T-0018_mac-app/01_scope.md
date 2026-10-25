# Scope

## In (Phase 1 bis 4)
1. **Gerüst** (diese Lieferung): Target `HousephoneMac` (SwiftUI, macOS 15, App Sandbox, Mikrofon, Netzwerk), nutzt `HousephoneKit`, baut in CI. Fenster liest einen Kopplungslink.
2. **Kopplung und Verbindung:** Gerät koppeln (Link einfügen, Heimnetz per Bonjour), Zugangsdaten im Schlüsselbund, `SignalingClient` hält die Verbindung.
3. **Anrufe:** Ausgehend und eingehend über die WebSocket-Audioverbindung der Bridge (G.711), Anrufbildschirm, Stumm, Tastenfeld, Mitteilung bei eingehendem Anruf, Klingelton.
4. **Daten:** FRITZ!Box-Telefonbuch und Anrufliste, Favoriten, Menüleisten-Symbol, `tel:`-Links.

## Außerhalb
- CallKit/PushKit auf dem Mac. Eingehende Anrufe kommen nur, solange die App läuft (Menüleiste, Start bei Anmeldung).
- Verwaltung der Bridge (Admin-Rolle), Modus ohne Bridge, Widgets.
- Mac Catalyst. Eigene SwiftUI-Mac-App, weil Anrufweg und Fenster anders sind.
