# Umsetzung

**Projekt**
- `ios/Config/Housephone.xcconfig` ist `baseConfigurationReference` der Projekt-Konfigurationen Debug/Release. `DEVELOPMENT_TEAM` aus der `project.pbxproj` entfernt (Projekt-Settings würden die xcconfig überdecken).
- `PRODUCT_BUNDLE_IDENTIFIER` = `$(HOUSEPHONE_BUNDLE_PREFIX).housephone[.watchkitapp|.localpush]`.
- `HousephoneWatch/Info.plist`: `WKCompanionAppBundleIdentifier` = `$(HOUSEPHONE_BUNDLE_PREFIX).housephone`. `Housephone/Info.plist`: URL-Typ-Name `$(PRODUCT_BUNDLE_IDENTIFIER).pairing`.
- `BridgeConnection.voipPushTopic` und `WatchBridge.voipPushTopic` = eigene Bundle-ID + `.voip`. Die Bridge prüft weiterhin gegen `apns.topic`.

**Skript** `Housephone einrichten.command`
- zsh, `cd` in den eigenen Ordner (Finder-Start), alternativer Bildschirm, Cursor aus, `trap` stellt das Terminal bei Ende/Ctrl-C wieder her.
- Schritte: 1 Mac/Xcode/Projekt prüfen und bisherige Werte zeigen · 2 Team (aus `security find-identity` + Zertifikats-OU/O, dedupliziert; „Andere …“ mit 10-Zeichen-Prüfung) · 3 Präfix (bisher, `com.<benutzer>`, „Andere …“ mit Reverse-DNS-Prüfung) · 4 Zusammenfassung mit allen Bundle-IDs und `apns.topic` · Fertig-Screen mit nächsten Schritten, „Projekt in Xcode öffnen“/„Beenden“.
- `--team X --prefix Y` schreibt ohne Rückfragen; ungültige Werte → Exit 2.

**CI** (`ios.yml`): Skript-Schritt vor dem Build (lehnt Unsinn ab, schreibt `org.example`), Build damit, danach Bundle-IDs und Companion-ID im fertigen Bundle geprüft. Workflow triggert auch auf Änderungen am Skript.
