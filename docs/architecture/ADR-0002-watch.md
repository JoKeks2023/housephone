# ADR-0002: Eigenständige Anrufe auf der Apple Watch

- Status: angenommen (2026-09-29)
- Baut auf ADR-0001 und Signalisierung v1 auf.

## Kontext

- CallKit-Anrufe einer reinen iPhone-App erscheinen nicht auf der Watch. Zum Annehmen am Handgelenk braucht es eine eigene watchOS-App mit CallKit und VoIP-Push (watchOS 9+).
- Apple TN3135 regelt Netzwerk auf watchOS:
  - **HTTPS über URLSession ist immer erlaubt.**
  - **WebSocket (`URLSessionWebSocketTask`), Network.framework und UDP** sind nur erlaubt, solange die App einen CallKit-Anruf führt.
- Für watchOS gibt es kein WebRTC.
- Laut watchOS-27-SDK sind verfügbar:
  - `AVAudioEngine.inputNode` (watchOS 4+)
  - `setVoiceProcessingEnabled` für Echounterdrückung (watchOS 6+)
  - `AVAudioConverter`
  - CallKit (watchOS 9+), PushKit, WatchConnectivity

## Entscheidung

1. **Eigene watchOS-App** `HousephoneWatch` (Bundle-ID `com.jorisconrad.housephone.watchkitapp`, als Companion der iPhone-App).
   - Sie nutzt CallKit und PushKit.
   - VoIP-Topic: `com.jorisconrad.housephone.watchkitapp.voip`.
   - Die iPhone-Logik aus `HousephoneKit` (Protokoll, `CallSession`, `PhoneNumber`) wird wiederverwendet; das Paket bekommt dafür watchOS als Plattform.
2. **Kopplung über das iPhone:** In den iPhone-Einstellungen gibt es „Apple Watch koppeln“.
   - Das iPhone fordert per WS `pair.companion` einen frischen Kopplungscode bei der Bridge an.
   - Den Code gibt es per WatchConnectivity an die Watch weiter.
   - Die Watch koppelt sich per **HTTPS** (`POST /v1/pair`) selbst und bekommt eigene Geräte-Zugangsdaten.
3. **Push-Token der Watch:** Die Watch meldet ihr Token per **HTTPS** (`PUT /v1/device`, Bearer-Auth) mit eigenem `pushTopic`.
   - Die Bridge verwendet das Topic pro Gerät; ohne Angabe gilt das konfigurierte Standard-Topic.
4. **Ton über die WebSocket-Verbindung**, nicht über UDP.
   - Während des Anrufs öffnet die Watch den WSS-Kanal. Die Bridge schickt statt `call.offer` ein `call.media` mit Codec PCMA, 8 kHz, 20-ms-Rahmen.
   - Der Ton läuft danach als **binäre WebSocket-Nachrichten**: 1 Byte Typ `0x01` plus 160 Byte A-law pro 20 ms.
   - Vorteile: keine zweite Portfreigabe, keine SRTP-Implementierung auf der Uhr, NAT-Traversal ist über Cloudflare Tunnel gelöst, die Verschlüsselung übernimmt TLS.
   - Kosten: TCP bringt bei Paketverlust Verzögerungsspitzen. Das fängt ein Jitter-Puffer (60–200 ms) auf der Uhr ab.
5. **Codec pro annehmendem Gerät:**
   - Die Bridge beantwortet das INVITE der FRITZ!Box erst beim `call.accept`, also mit dem Codec des Geräts, das annimmt: iPhone G.722, Watch PCMA.
   - Ausgehend von der Watch bietet sie PCMA an.
   - Die Bridge transkodiert nie.
6. **Geräte-Fähigkeiten:** `hello` bekommt `mediaCapabilities`, entweder `["webrtc"]` (iPhone) oder `["websocket-pcma"]` (Watch).
   - Fehlt das Feld, gilt `["webrtc"]`. Damit bleibt die iPhone-App v0.1 kompatibel.
7. **Audio auf der Uhr:**
   - `AVAudioEngine` mit Voice Processing; die Session aktiviert CallKit.
   - Mikrofon über `AVAudioConverter` auf 8 kHz/Int16 → A-law → Rahmen.
   - Wiedergabe über einen `AVAudioPlayerNode` mit 8-kHz-Float-Puffern hinter dem Jitter-Puffer.

## Verworfene Alternativen

| Alternative | Warum nicht |
|---|---|
| SRTP über UDP direkt von der Uhr | Braucht eine zweite Portfreigabe, eine SRTP-Implementierung in Swift und Latching. Mehr Aufwand ohne klaren Qualitätsgewinn für einen Uhrenlautsprecher. |
| Ton über das iPhone weiterleiten | Die Uhr soll auch ohne iPhone in der Nähe telefonieren (LTE/WLAN). |
| G.722 auf der Uhr | Für v1 zu viel Codec-Portierung. Der Lautsprecher der Uhr profitiert kaum. Später möglich (libg722 ist gemeinfrei). |
