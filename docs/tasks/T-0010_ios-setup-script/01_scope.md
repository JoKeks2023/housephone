# Umfang

**Drin**
- `ios/Config/Housephone.xcconfig` als Basis-Konfiguration des Projekts (Debug und Release) mit `DEVELOPMENT_TEAM` und `HOUSEPHONE_BUNDLE_PREFIX`, dazu `#include? "Local.xcconfig"` (gitignored)
- Bundle-IDs aller drei Targets und `WKCompanionAppBundleIdentifier` aus dem Präfix; VoIP-Topics in App und Watch aus der eigenen Bundle-ID; URL-Typ-Name aus der Bundle-ID
- `Housephone einrichten.command` im Repo-Ordner: zsh, ohne Abhängigkeiten, TUI mit Pfeiltasten-Menüs, Teams aus den Signier-Zertifikaten im Schlüsselbund, Präfix-Vorschläge, „Andere …“ mit Prüfung, Zusammenfassung, APNs-Topic für die Bridge, Projekt öffnen
- Nicht-interaktiver Modus `--team … --prefix …` für CI
- Doku: README Schritt 6, `ios/README.md`, `bridge/README.md`, Add-on-Übersetzungen

**Nicht drin**
- Local-Push-Extension einbetten (nur Hinweis auf ADR-0005)
- Keychain-Service-Namen (`com.jorisconrad.housephone.direct`/`.bridge`): reine Bezeichner, durch die Bundle-ID ohnehin pro App getrennt
- Logger-Subsysteme
