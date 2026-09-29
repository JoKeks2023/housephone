# Scope

## Drin

- **Bridge:**
  - `mediaCapabilities`/`pushTopic` pro Gerät
  - HTTPS `POST /v1/pair`, `PUT`/`DELETE /v1/device`
  - `pair.companion.request` → `pair.companion`
  - Medienweg `websocket-pcma`: `call.media` plus binäre Audio-Rahmen ↔ RTP
  - SIP-Antwort mit dem Codec des annehmenden Geräts; ausgehend von der Watch nur PCMA
  - Push mit dem Topic des Geräts
- **HousephoneKit:**
  - watchOS als Plattform
  - neue Nachrichten und Felder
  - G.711 A-law
  - Audio-Rahmen (Binärformat)
  - Jitter-Puffer
  - HTTPS-Kopplungs- und Geräte-Client
- **iOS-App:**
  - „Apple Watch koppeln“ in den Einstellungen: Code bei der Bridge anfordern und per WatchConnectivity übergeben
  - Kopplungsstatus der Watch anzeigen
- **watchOS-App `HousephoneWatch`** (Companion, watchOS 26):
  - CallKit + PushKit
  - WSS nur während des Anrufs
  - Audio mit AVAudioEngine (Voice Processing), A-law ↔ PCM
  - Oberfläche: Anrufbildschirm (Stumm, Lautstärke, Auflegen), Tastenfeld/Wahl, letzte Anrufe, Status/Kopplung
  - Deutsch + Englisch
- **CI:** Die Watch-App baut mit dem iOS-Scheme; Kit-Tests laufen auch für watchOS-relevanten Code.

## Nicht drin

- G.722 auf der Uhr
- Komplikationen/Widgets
- Kontakte-Sync auf die Uhr (die Uhr zeigt Name aus Push/Bridge und Nummer)
- eigenständige Watch-App ohne iPhone-Installation
