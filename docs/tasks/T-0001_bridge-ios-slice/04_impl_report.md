# Umsetzungsbericht T-0001

**Stand:** 2026-09-29

| PR | Inhalt | Merge-Commit |
|---|---|---|
| #3 | Bridge (`bridge/`), Grundlagen-Doku, Bridge-CI | `c6246ca7` |
| #4 | iOS-App (`ios/`), `HousephoneKit`, iOS-CI, `docs/setup.md` | `ca9b4720` |

## Bridge (Go)

- **Pakete unter `bridge/internal/`:**
  - `app`: Verdrahtung und End-to-End-Tests
  - `calls`: Anruf-Manager, ein Actor pro Anruf
  - `sipleg`: FRITZ!Box über diago/sipgo
  - `media`: pion-WebRTC über einen UDP-Port
  - `signaling`: WebSocket, Kopplung, Auth
  - `push`: APNs
  - `store`: JSON-Dateien
  - dazu `protocol`, `codec`, `rtpx`, `auth`, `config`, `fakefritz`, `testdevice`, `version`
- **CLI:** `housephone-bridge serve|pair|devices list/remove|version`
- **Deployment:** Dockerfile (statisch, non-root), `docker-compose.example.yml` mit cloudflared, Anleitung in `bridge/README.md`
- **Abhängigkeiten:** diago v0.40.0, sipgo v1.6.0, pion webrtc v4.2.19 / ice v4.4.0 (gepinnt), apns2

## iOS-App (Swift 6, iOS 26)

- **`HousephoneKit`:** Protokoll, `SignalingClient`, `CallSession`-Zustandsmaschine, Kopplungslink, Schlüsselbund, Push-Payload, Nummern, Freiton
- **App:**
  - `CallCenter`: CallKit und PushKit
  - `MediaEngine`: WebRTC 153
  - `BridgeConnection`
  - Screens: Kopplung, Anrufe, Tastenfeld, Kontakte, Einstellungen, Anrufbildschirm
  - Deutsch + Englisch
- **XcodeGen:** `ios/project.yml`, das Projekt wird nicht eingecheckt

## Abweichungen und Präzisierungen gegenüber dem Plan

Alle stehen in `docs/protocol/signaling-v1.md`:

- `call.attach` ist idempotent auf **einer** PeerConnection.
- Die Bridge schickt selbst ICE-Restart-Offers nach ICE-Fehlern. Die App wartet deshalb 15 s auf Erholung, bevor sie auflegt.
- Neu: `device.unpair`.
- 480 statt 486, wenn kein Gerät erreichbar ist.
- Beendete Anrufe bleiben 2 Minuten als „Tombstones“ bekannt.
- Zeitstempel ohne Sekundenbruchteile.
- Beim Wählen gibt es keinen zweiten Codec-Versuch nach 488 (erst in v2).

## Nicht umgesetzt (bewusst, siehe `01_scope.md`)

- Halten, Makeln
- TURN-Fallback
- Trickle-ICE
- TR-064
- Watch (folgt in T-0002)
