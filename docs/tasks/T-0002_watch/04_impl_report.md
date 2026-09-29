# Umsetzungsbericht T-0002

**Stand:** 2026-09-29

| PR | Inhalt |
|---|---|
| #6 | Watch-App `HousephoneWatch`, Kit v1.1, iPhone-Kopplung der Watch, Bridge v1.1 (`websocket-pcma`, HTTPS-Endpunkte, Companion-Kopplung, Codec pro annehmendem Gerät) |
| dieser PR | `GET /v1/calls/{callId}` (Anrufstatus per HTTPS beim Klingeln), gemeinsames Entkoppeln iPhone → Watch, „Kopplung aufheben“ auf der Uhr |

## Watch-App

- CallKit + PushKit (`UIBackgroundModes` voip/audio, `aps-environment`).
- WebSocket nur während eines Anrufs.
- `AVAudioEngine` mit Echounterdrückung; A-law in 20-ms-Rahmen über binäre WebSocket-Nachrichten; adaptiver Jitter-Puffer.
- Oberfläche: Start, letzte Anrufe, Tastenfeld, Anrufbildschirm mit Lautstärke über die Digital Crown. Deutsch + Englisch.
- Beim Klingeln fragt die Uhr alle 2 s per HTTPS den Anrufstatus ab. Die Klingel-Timeouts (60/180 s) bleiben als Rückfallebene.

## Bridge v1.1

- Pro Gerät: `mediaCapabilities`, `pushTopic` (validiert).
- HTTPS: `POST /v1/pair`, `PUT`/`DELETE /v1/device`, `GET /v1/calls/{id}`.
- `pair.companion`.
- `wsaudio`: Gerät → FRITZ!Box getaktet, FRITZ!Box → Gerät in 160-Byte-Rahmen neu zerlegt.
- Die SIP-Antwort nutzt den Codec des annehmenden Geräts.

## Abweichungen und Präzisierungen

Alle stehen in `docs/protocol/signaling-v1.md`:
- `hello` ohne die neuen Felder setzt die Standardwerte zurück.
- Ein Gerät mit `webrtc` **und** `websocket-pcma` bekommt WebRTC.
- Ein `call.answer` von einem `websocket-pcma`-Gerät wird mit `bad_request` beantwortet.
- Die Watch wird nicht gepusht, wenn das INVITE kein PCMA anbietet.
