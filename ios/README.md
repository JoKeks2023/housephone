# Housephone für iOS und watchOS

SwiftUI-App (iOS 26+) mit CallKit und PushKit, dazu eine eigenständige Apple-Watch-App (watchOS 26+). Sie telefoniert über die Bridge (`bridge/`) mit der FRITZ!Box. Architektur: `docs/architecture/ADR-0001-bridge-architektur.md`, Protokoll: `docs/protocol/signaling-v1.md`.

## Bauen

```sh
brew install xcodegen      # einmalig
cd ios
xcodegen generate          # erzeugt Housephone.xcodeproj (nicht eingecheckt)
open Housephone.xcodeproj
```

- Auf ein **echtes iPhone** bauen. CallKit, PushKit und Audio lassen sich im Simulator nicht sinnvoll testen.
- Signing ist automatisch mit Team `T9CA6D7T8N`.
- Bundle-IDs: `com.jorisconrad.housephone` (iPhone) und `com.jorisconrad.housephone.watchkitapp` (Watch).
- Die Capability „Push Notifications“ muss im Developer-Account für beide App-IDs aktiv sein. XcodeGen setzt `aps-environment` in den Entitlements.
- Die Watch-App wird mit dem Scheme **Housephone** gebaut und in die iPhone-App eingebettet (`PlugIns/`). `scripts/fix-watch-embed.sh` läuft dafür automatisch nach `xcodegen generate` (XcodeGen 2.46 bettet sonst in das veraltete `Watch/` ein).

## Aufbau

| Pfad | Inhalt |
|---|---|
| `Packages/HousephoneKit` | Plattformneutral und lokal testbar (`swift test`): Protokoll-Nachrichten, `SignalingClient` (WebSocket inkl. Binär-Audio, Reconnect, Kopplung), `BridgeHTTPClient` (HTTPS-Kopplung der Watch, FRITZ!Box-Telefonbuch mit ETag, Anrufliste), `FritzBoxResource` (Cache auf der Platte), `PhonebookNameIndex` (Anrufername), `CallSession` (Zustandsmaschine eines Anrufs), G.711 A-law, Audio-Rahmen, Jitter-Puffer, WatchConnectivity-Nachrichten, Kopplungslink, Schlüsselbund, Push-Payload, Freiton |
| `Housephone/Calling` | `CallCenter` (CallKit + PushKit + Ablaufsteuerung), `MediaEngine` (WebRTC), `RingbackPlayer` |
| `Housephone/Bridge` | `BridgeConnection`: Kopplung, Verbindungsstatus, Push-Token, Kopplungscode für die Watch; `WatchLink`: WatchConnectivity |
| `Housephone/Features` | Tastenfeld, Anrufe (Housephone/FRITZ!Box), Kontakte (FRITZ!Box/iPhone), Einstellungen, Onboarding/Kopplung, Anrufbildschirm |
| `Housephone/Design` | Design-Tokens (Designsprache jorisconrad) und gemeinsame Bausteine |
| `Housephone/Resources` | Assets (Icon, Akzentfarbe), String-Kataloge (Deutsch als Quelle, Englisch) |
| `HousephoneWatch/Calling` | `WatchCallCenter` (CallKit + PushKit, eine WebSocket-Verbindung pro Anruf), `CallAudio` (AVAudioEngine mit Echounterdrückung, A-law, Jitter-Puffer, Freiton) |
| `HousephoneWatch/Bridge` | `WatchBridge` (Zugangsdaten, Push-Token per HTTPS), `PhoneLink` (WatchConnectivity) |
| `HousephoneWatch/Features` | Kopplungshinweis, Start (Bereitschaft, verpasste FRITZ!Box-Anrufe, letzte Anrufe), Tastenfeld, FRITZ!Box-Kontakte, Anrufbildschirm mit Digital-Crown-Lautstärke |

### Warum die Watch anders telefoniert

watchOS erlaubt WebSocket, Network.framework und UDP nur während eines CallKit-Anrufs (Apple TN3135), und WebRTC gibt es für watchOS nicht. Deshalb:

- Kopplung und Push-Token laufen außerhalb von Anrufen über HTTPS (`POST /v1/pair`, `PUT /v1/device`).
- Jeder Anruf öffnet eine eigene WebSocket-Verbindung und schließt sie danach wieder.
- Der Ton läuft als binäre WebSocket-Nachrichten (G.711 A-law, 20-ms-Rahmen, `websocket-pcma`).

Details: `docs/architecture/ADR-0002-watch.md`.

## Tests

```sh
cd ios/Packages/HousephoneKit && swift test
```

Die CI (`.github/workflows/ios.yml`) führt die Kit-Tests aus, baut die App unsigniert für `generic/platform=iOS` und prüft, dass die Watch-App kompiliert und unter `PlugIns/` eingebettet ist.
