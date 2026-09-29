# Housephone für iOS

SwiftUI-App (iOS 26+) mit CallKit und PushKit. Sie telefoniert über die Bridge (`bridge/`) mit der FRITZ!Box. Architektur: `docs/architecture/ADR-0001-bridge-architektur.md`, Protokoll: `docs/protocol/signaling-v1.md`.

## Bauen

```sh
brew install xcodegen      # einmalig
cd ios
xcodegen generate          # erzeugt Housephone.xcodeproj (nicht eingecheckt)
open Housephone.xcodeproj
```

- Auf ein **echtes iPhone** bauen. CallKit, PushKit und Audio lassen sich im Simulator nicht sinnvoll testen.
- Signing ist automatisch mit Team `T9CA6D7T8N`.
- Bundle-ID `com.jorisconrad.housephone`.
- Die Capability „Push Notifications“ muss im Developer-Account für die App-ID aktiv sein. XcodeGen setzt `aps-environment` in den Entitlements.

## Aufbau

| Pfad | Inhalt |
|---|---|
| `Packages/HousephoneKit` | Plattformneutral und lokal testbar (`swift test`): Protokoll-Nachrichten, `SignalingClient` (WebSocket, Reconnect, Kopplung), `CallSession` (Zustandsmaschine eines Anrufs), Kopplungslink, Schlüsselbund, Push-Payload, Freiton |
| `Housephone/Calling` | `CallCenter` (CallKit + PushKit + Ablaufsteuerung), `MediaEngine` (WebRTC), `RingbackPlayer` |
| `Housephone/Bridge` | `BridgeConnection`: Kopplung, Verbindungsstatus, Push-Token |
| `Housephone/Features` | Tastenfeld, Anrufe, Kontakte, Einstellungen, Onboarding/Kopplung, Anrufbildschirm |
| `Housephone/Design` | Design-Tokens (Designsprache jorisconrad) und gemeinsame Bausteine |
| `Housephone/Resources` | Assets (Icon, Akzentfarbe), String-Kataloge (Deutsch als Quelle, Englisch) |

## Tests

```sh
cd ios/Packages/HousephoneKit && swift test
```

Die CI (`.github/workflows/ios.yml`) führt die Kit-Tests aus und baut die App unsigniert für `generic/platform=iOS`.
