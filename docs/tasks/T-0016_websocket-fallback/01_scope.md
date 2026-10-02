# Umfang

- Bridge (`internal/calls`): Wechsel eines WebRTC-Beins auf `websocket-pcma`, wenn der Peer nicht verbindet; PCMA-only-Offer für Geräte am öffentlichen Zugang.
- Protokoll: Erweiterung v1.4 in `docs/protocol/signaling-v1.md`.
- iPhone-App: meldet `websocket-pcma`, schaltet bei `call.media` von WebRTC auf `CallAudio` um.

Nicht im Umfang: Codec-Wandlung (G.722 ↔ PCMA), TURN-Server, automatische Portfreigabe per UPnP.
