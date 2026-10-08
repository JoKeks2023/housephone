# Housephone für iOS und watchOS

SwiftUI-App (iOS 17+, iPhone und iPad; Liquid Glass ab iOS 26) mit CallKit und PushKit, dazu eine eigenständige Apple-Watch-App (watchOS 10+). Sie telefoniert über die Bridge (`bridge/`) mit der FRITZ!Box. Architektur: `docs/architecture/ADR-0001-bridge-architektur.md`, Protokoll: `docs/protocol/signaling-v1.md`.

## Bauen

Einmalig `Housephone einrichten.command` im Repo-Ordner doppelklicken (oder im Terminal starten), dann:

```sh
open ios/Housephone.xcodeproj
```

- Auf ein **echtes iPhone** bauen. CallKit, PushKit und Audio lassen sich im Simulator nicht sinnvoll testen.
- **Team und Bundle-IDs** stehen nicht in der `project.pbxproj`, sondern in `ios/Config/Housephone.xcconfig` (Standard: Team `T9CA6D7T8N`, Präfix `com.jorisconrad`). Eigene Werte schreibt das Setup-Skript nach `ios/Config/Local.xcconfig` (nicht eingecheckt); die überschreibt die Standards. Ohne Rückfragen: `./"Housephone einrichten.command" --team ABCDE12345 --prefix org.example`.
- Bundle-IDs: `<präfix>.housephone` (iPhone), `<präfix>.housephone.watchkitapp` (Watch), `<präfix>.housephone.localpush` (Local-Push-Extension). Die App leitet ihr VoIP-Topic aus der eigenen Bundle-ID ab (`<bundle-id>.voip`); in der Bridge muss deshalb `apns.topic` = `<präfix>.housephone.voip` sein.
- Signing in Xcode bleibt automatisch. Team oder Präfix nicht im Xcode-Reiter „Signing & Capabilities“ ändern: Das schreibt in die `project.pbxproj` und überdeckt die xcconfig. Stattdessen das Skript erneut starten.
- Die Capability „Push Notifications“ muss im Developer-Account für beide App-IDs aktiv sein. `aps-environment` steht in den Entitlements.
- App Group `group.<präfix>.housephone` (`HOUSEPHONE_APP_GROUP` in der xcconfig) und Siri stehen in den Entitlements. Die automatische Signierung legt beides im Developer-Account an. Die App leitet die Gruppe zur Laufzeit aus ihrer Bundle-ID ab.
- Die Watch-App wird mit dem Scheme **Housephone** gebaut und in die iPhone-App eingebettet (`Watch/`; unter `PlugIns/` lehnt App Store Connect den Upload ab, ITMS-90680).
- Das Xcode-Projekt ist eingecheckt und nutzt synchronisierte Ordner: Neue Dateien in `Housephone/`, `HousephoneWatch/` und `HousephoneLocalPush/` erscheinen automatisch in Xcode, ohne Generator. Einstellungen, Info.plist und Entitlements änderst du direkt in Xcode.

## Aufbau

| Pfad | Inhalt |
|---|---|
| `Packages/HousephoneKit` | Plattformneutral und lokal testbar (`swift test`): Protokoll-Nachrichten, HP2 (Anmeldung v2, ADR-0004: signierte Anfragen, gepinnter Bridge-Schlüssel, verschlüsselte Rahmen), `DeviceKey` (Secure Enclave), `SignalingClient` (WebSocket inkl. Binär-Audio, Reconnect), `BridgeHTTPClient` (Kopplung, FRITZ!Box-Telefonbuch mit ETag, Anrufliste), `FritzBoxResource` (Cache auf der Platte), `PhonebookNameIndex` (Anrufername), `CallSession` (Zustandsmaschine eines Anrufs), G.711 A-law, Audio-Rahmen, Jitter-Puffer, WatchConnectivity-Nachrichten, Kopplungslink, Schlüsselbund, Push-Payload, Freiton |
| `Housephone/Calling` | `CallCenter` (CallKit + PushKit + Ablaufsteuerung), `MediaEngine` (WebRTC), `RingbackPlayer` |
| `Housephone/Bridge` | `BridgeConnection`: Kopplung, Verbindungsstatus, Push-Token, Kopplungscode für die Watch; `WatchLink`: WatchConnectivity |
| `Housephone/Features` | Tastenfeld, Anrufe (Housephone/FRITZ!Box), Kontakte (FRITZ!Box/iPhone), Einstellungen, Onboarding/Kopplung, Anrufbildschirm |
| `Housephone/Design` | Design-Tokens (Designsprache jorisconrad) und gemeinsame Bausteine |
| `Housephone/Resources` | Assets (Icon, Akzentfarbe), String-Kataloge (Deutsch als Quelle, Englisch) |
| `HousephoneWatch/Calling` | `WatchCallCenter` (CallKit + PushKit, eine WebSocket-Verbindung pro Anruf), `CallAudio` (AVAudioEngine mit Echounterdrückung, A-law, Jitter-Puffer, Freiton) |
| `HousephoneWatch/Bridge` | `WatchBridge` (Zugangsdaten, Push-Token per HTTPS), `PhoneLink` (WatchConnectivity) |
| `HousephoneWatch/Features` | Kopplungshinweis, Start (Bereitschaft, verpasste FRITZ!Box-Anrufe, letzte Anrufe), Tastenfeld, FRITZ!Box-Kontakte, Anrufbildschirm mit Digital-Crown-Lautstärke |

### Systemintegration (T-0017)

- **Siri:** „Ruf <Name> mit Housephone an“ läuft über `INStartCallIntent` in der App (`Integration/StartCallIntentHandler.swift`), auch in CarPlay. Siri kennt iPhone-Kontakte selbst, Favoriten und FRITZ!Box-Telefonbuch sucht die App.
- **Kurzbefehle und App Shortcuts:** `Integration/AppIntents.swift` (Favorit anrufen, Nummer anrufen, Tastenfeld, verpasste Anrufe).
- **Schnellaktionen:** bis zu vier Favoriten am App-Icon (`Integration/QuickActions.swift`).
- **Schnappschuss für Erweiterungen:** `SnapshotPublisher` schreibt Favoriten und letzte Anrufe als JSON in die App Group (`SharedSnapshot` in HousephoneKit). Erweiterungen lesen nur und telefonieren nie selbst.
- **Deep Links:** `housephone://call?number=…`, `housephone://keypad`, `housephone://recents?missed=1`. Anruf-Links ohne den Schlüssel aus der App Group (`DeepLinkKey`) fragen vor dem Wählen nach.

### Warum die Watch anders telefoniert

watchOS erlaubt WebSocket, Network.framework und UDP nur während eines CallKit-Anrufs (Apple TN3135), und WebRTC gibt es für watchOS nicht. Deshalb:

- Kopplung und Push-Token laufen außerhalb von Anrufen über HTTPS (`POST /v1/pair`, `PUT /v1/device`). iPhone und Watch haben je einen eigenen Geräteschlüssel; es gibt kein Geheimnis im Schlüsselbund, nur den gepinnten Bridge-Schlüssel.
- Jeder Anruf öffnet eine eigene WebSocket-Verbindung und schließt sie danach wieder.
- Der Ton läuft als binäre WebSocket-Nachrichten (G.711 A-law, 20-ms-Rahmen, `websocket-pcma`).

Details: `docs/architecture/ADR-0002-watch.md`.

## Tests

```sh
cd ios/Packages/HousephoneKit && swift test
```

Die CI (`.github/workflows/ios.yml`) führt die Kit-Tests aus, baut die App unsigniert für `generic/platform=iOS` und prüft, dass die Watch-App kompiliert und unter `Watch/` eingebettet ist.
