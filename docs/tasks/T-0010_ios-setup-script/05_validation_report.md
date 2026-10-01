# Validierung

| Prüfung | Ergebnis |
|---|---|
| `xcodebuild -showBuildSettings` ohne `Local.xcconfig` | Team `T9CA6D7T8N`, `com.jorisconrad.housephone[.watchkitapp]` |
| dto. mit `--team ABCDE12345 --prefix org.example` | Team `ABCDE12345`, `org.example.housephone` |
| TUI im Pseudo-Terminal (`script`), Tasten Enter/↓/Enter… | alle Screens erscheinen, Teams aus dem Schlüsselbund erkannt, `Local.xcconfig` geschrieben, Terminal (Alt-Screen, Cursor) wiederhergestellt |
| CI iOS, Run 36831750682 (12c25bb4) | grün: Skript lehnt ungültige Werte ab, App-Build mit `org.example`, Bundle-IDs und `WKCompanionAppBundleIdentifier` im Bundle geprüft |
| CI Bridge, Add-on | grün |

Erster Lauf von „HousephoneKit tests“ schlug in `SIPUserAgentTests.wrongPasswordFailsWithoutRetrying` fehl (3 statt 2 REGISTER). HousephoneKit ist hier nicht geändert; der Neustart war grün. Als #33 erfasst.

**Nicht verifiziert:** Doppelklick im Finder, Darstellung in Terminal.app (Farben, Pfeiltasten von echter Tastatur), signierter Build mit fremdem Team.
