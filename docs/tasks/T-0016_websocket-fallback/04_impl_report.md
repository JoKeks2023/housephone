# Umsetzung

| Teil | Änderung |
|---|---|
| Bridge `store` | `Device.CanFallBackToWebSocketAudio()` |
| Bridge `calls` | `leg.fallback`, `armFallback` (Timer `MediaFallbackTimeout`, Standard 5 s, ab `call.answer`), `fallBackToWebSocket` (Peer schließen, `call.media`, Relay neu), bei `PeerFailed` statt ICE-Restart; ausgehend vom öffentlichen Zugang nur PCMA; Event `media_fallback` |
| Bridge `signaling` | `session.Private()` für die Codec-Wahl |
| Protokoll | v1.4 in `docs/protocol/signaling-v1.md` |
| iPhone | `mediaCapabilities` `[webrtc, websocket-pcma]`; `CallCenter.startSocketAudio`/`stopSocketAudio` mit `CallAudio`; `BridgeConnection.audio` (`AudioRouter`) als einziger Leser des Audio-Streams; `MediaEngine.suspendAudio()` |
| Tests | `bridge/internal/calls/fallback_test.go` (4 Fälle), `CallSessionWebSocketMediaTests` (2 Fälle) |

Beim Umschalten kann ein 20-ms-Paket der FRITZ!Box verloren gehen (der alte Relay-Leser nimmt es noch).
