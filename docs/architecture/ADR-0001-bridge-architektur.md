# ADR-0001: Bridge-Architektur für iPhone, Apple Watch und FRITZ!Box

- Status: angenommen (2026-09-29)

## Kontext

- Ziel: Über die FRITZ!Box mit iPhone (und später Apple Watch) telefonieren – zu Hause **und unterwegs, ohne VPN auf dem Telefon**.
- CallKit ist Pflicht.
- iOS hält im Hintergrund keine SIP-Verbindung offen. Eingehende Anrufe gibt es nur, wenn ein **VoIP-Push (PushKit)** die App weckt. Die FRITZ!Box kann keine Pushes senden.
- AVMs eigene FRITZ!App Fon klingelt deshalb seit 2022 nur noch im Heim-WLAN.
- Die Apple Watch zeigt CallKit-Anrufe reiner iPhone-Apps nicht an. Sie braucht eine eigene watchOS-App (watchOS 9+, CallKit + VoIP-Push). Netzwerk ist dort nur während eines laufenden CallKit-Anrufs erlaubt (TN3135).
- Vorhanden:
  - FRITZ!Box 6591 Cable (Vodafone), FRITZ!OS 8.25
  - Vodafone Red Business 300 mit öffentlicher IPv4, Portfreigaben möglich
  - ein dauerhaft laufender Server im Heimnetz
  - bezahlter Apple-Developer-Account (Team `T9CA6D7T8N`)

## Entscheidung

Ein eigener Dienst **„Bridge“** läuft auf dem Heimserver und vermittelt zwischen FRITZ!Box und Geräten.

```
          Heimnetz                                   Internet / WLAN
┌──────────────────────────────────┐
│ FRITZ!Box ◄─SIP/RTP (UDP)─► Bridge│◄── WSS (Signalisierung) ── Cloudflare Tunnel ◄── iPhone / Watch
│                                   │◄── DTLS-SRTP (WebRTC, 1 UDP-Port, Portfreigabe) ──►
│                                   │─── VoIP-Push (APNs, HTTP/2) ──► Apple ──► iPhone / Watch
└──────────────────────────────────┘
```

1. **FRITZ!Box-Seite:** Die Bridge meldet sich als ganz normales **IP-Telefon** an der FRITZ!Box an (SIP über UDP, nur im Heimnetz). An der FRITZ!Box ist keine „Anmeldung aus dem Internet“ nötig.
2. **Geräte-Seite:** Die Geräte sprechen nicht SIP, sondern ein eigenes, schlankes **JSON-Protokoll über WebSocket** (siehe `docs/protocol/signaling-v1.md`). Der Ton läuft über **WebRTC**.
3. **Kein Transcoding:** Beide Seiten eines Gesprächs verwenden denselben Codec. Die Bridge reicht RTP-Nutzdaten nur durch.
   - Bevorzugt **G.722** (HD-Sprache, 16 kHz), sonst PCMA, sonst PCMU.
   - Die FRITZ!Box unterstützt alle drei für IP-Telefone, WebRTC ebenfalls.
4. **Wecken:** Bei einem eingehenden Anruf schickt die Bridge einen **VoIP-Push über APNs** direkt an alle gekoppelten Geräte. Diese melden den Anruf sofort an CallKit, bauen dann die WebSocket-Verbindung auf und hängen sich an den Anruf.
5. **Erreichbarkeit von außen:**
   - **Signalisierung:** über **Cloudflare Tunnel** (WSS, TLS am Cloudflare-Edge, kein offener TCP-Port).
   - **Ton:** über **einen** freigegebenen UDP-Port. pion nutzt dafür ICE-UDP-Mux und NAT-1:1 mit der öffentlichen IP.
   - **Im Heim-WLAN:** Dort greifen die lokalen ICE-Kandidaten und der Ton bleibt im LAN.
6. **Geräte-Kopplung:** Die Bridge erzeugt auf dem Server per CLI einen einmaligen, kurzlebigen Kopplungscode als QR-Code. Die App tauscht ihn gegen eine Geräte-ID und ein Geräte-Geheimnis. Das Geheimnis liegt auf dem Telefon im Schlüsselbund und auf der Bridge nur als SHA-256-Hash.
7. **Technik Bridge:** Go.
   - `emiago/diago` + `emiago/sipgo` für SIP (MPL-2.0 / BSD-2)
   - `pion/webrtc/v4` für WebRTC (MIT)
   - `sideshow/apns2` für Pushes (MIT)
   - ergibt ein statisches Binary, Deployment per Docker
8. **Technik iOS:**
   - SwiftUI, Swift 6, ab iOS 17 (iPhone und iPad, Watch ab watchOS 10); Liquid Glass ab iOS 26, darunter Materialien
   - CallKit + PushKit
   - WebRTC über `stasel/WebRTC` (SPM-Binary, BSD)
   - Protokoll- und Zustandslogik im lokalen Swift-Paket `HousephoneKit` (plattformneutral, lokal testbar)
9. **Watch:** später.
   - WebRTC gibt es für watchOS nicht.
   - Das Protokoll ist so geschnitten, dass die Bridge für die Watch zusätzlich einen schlanken RTP/SRTP-Medienweg anbieten kann, ohne dass sich an den Nachrichten etwas ändert.

## Verworfene Alternativen

| Alternative | Warum nicht |
|---|---|
| VPN (WireGuard) auf dem Telefon | Vom Nutzer ausgeschlossen. Löst außerdem das Wecken nicht, weil die App trotzdem nicht im Hintergrund angemeldet bleiben darf. |
| Local Push Connectivity (`NEAppPushProvider`) | Funktioniert nur in festgelegten WLANs, also nicht unterwegs. Wird durch die Bridge überflüssig. |
| FRITZ!Box „Anmeldung aus dem Internet erlauben“ | Löst nur ausgehende Anrufe. AVM rät ab, weil der SIP-Port offen im Internet steht. |
| SIP über WSS zur App (z. B. Asterisk/flexisip) | Erfordert eine SIP-Implementierung in Swift. Das eigene JSON-Protokoll ist deutlich schlanker und transportiert App-Funktionen (Kopplung, Push-Token, Status) direkt mit. |
| PJSIP in der App | GPL bzw. kommerzielle Lizenz. Kein NAT-Traversal ohne eigene TURN-Infrastruktur. |
| Cloudflare Tunnel für alles | Der Tunnel trägt bei öffentlichen Hostnamen kein UDP. |

## Folgen

- Es braucht einen APNs-Auth-Key (`.p8`) im Apple-Developer-Account und einen Cloudflare Tunnel, oder alternativ Portfreigabe plus eigenes TLS-Zertifikat.
- Die Bridge ist ein Single Point of Failure. Fällt sie aus, klingeln die Geräte nicht; die übrigen Telefone an der FRITZ!Box sind davon nicht betroffen.
- Mobilfunknetze mit restriktivem UDP könnten den Medienweg blockieren. Der Fallback wäre ein TURN-Relay (z. B. Cloudflare Realtime TURN). Das ist im Protokoll über `iceServers` bereits vorgesehen, in v1 aber nicht umgesetzt.
