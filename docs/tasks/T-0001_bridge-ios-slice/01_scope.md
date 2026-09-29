# Scope

## Drin

- **Bridge (`bridge/`, Go)**
  - Registrierung an der FRITZ!Box
  - eingehende und ausgehende Anrufe
  - Geräte-Kopplung (CLI + QR)
  - VoIP-Push über APNs
  - WebRTC-Medienweg mit RTP-Durchreichung (G.722/PCMA/PCMU)
  - WebSocket-Signalisierung v1
  - Docker-Deployment, Beispielkonfiguration
- **iOS-App (`ios/`)**
  - SwiftUI, iOS 26, Liquid Glass
  - Kopplung per QR-Code/Link
  - CallKit + PushKit
  - Tastenfeld, Anrufliste, Kontakte-Auswahl, Anrufbildschirm (Stumm, Lautsprecher/Route, Tastentöne, Auflegen)
  - Einstellungen mit Bridge-Status
  - Anrufe aus der iPhone-Telefon-App/Kontakten heraus (`INStartCallIntent`)
  - Deutsch und Englisch
- **`HousephoneKit`:** Protokoll, Signalisierungs-Client, Kopplungslink, Zustandslogik – mit Unit-Tests.
- **CI (GitHub Actions):**
  - Bridge: vet, test, build
  - Kit: `swift test`
  - App: unsignierter Build für iOS-Geräte
- **Einrichtungsanleitung** `docs/setup.md`: FRITZ!Box, APNs-Key, Cloudflare Tunnel, Portfreigabe, Kopplung.

## Nicht drin (später)

- watchOS-App (T-0002)
- Halten/Makeln/Konferenz
- TURN-Fallback
- Trickle-ICE
- Codec-Neuverhandlung
- TR-064 (FRITZ!Box-Anrufliste und -Telefonbuch)
- App-Store-Veröffentlichung
