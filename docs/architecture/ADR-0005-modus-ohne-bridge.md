# ADR-0005: Modus ohne Bridge („Direkt mit FRITZ!Box“)

- Status: vorgeschlagen (2026-09-29), Branch `feat/standalone`
- Ergänzt ADR-0001 (Bridge-Architektur); ersetzt sie nicht.

## Kontext

- Nicht jeder will einen Server mit Docker betreiben. Wer nur zu Hause telefoniert, braucht die Bridge eigentlich nicht: Die FRITZ!Box ist selbst ein SIP-Registrar (IP-Telefon) und spricht TR-064.
- Ohne Bridge fehlt aber der VoIP-Push. Die FRITZ!Box kann keine APNs-Pushes schicken; eine iOS-App im Hintergrund hält keinen SIP-Socket offen.
- Apple bietet dafür **Local Push Connectivity** (`NEAppPushProvider`, iOS 14+):
  - Eine Network Extension hält im festgelegten WLAN (`matchSSIDs`) die Verbindung zum Server und darf per `reportIncomingCall` einen CallKit-Anruf auslösen.
  - Das Entitlement `com.apple.developer.networking.networkextension` mit dem Wert `app-push-provider` ist **nicht frei verfügbar**. Der Account-Inhaber muss es bei Apple beantragen. Nach der Freigabe braucht es explizite App-IDs, Provisioning-Profile mit dem Zusatz-Entitlement und manuelles Signing. Quellen: Apple-Doku „Local Push Connectivity“ (Hinweis „Important“), WWDC20 Session 10113, Developer-Forum Thread 692050.

## Entscheidung

1. **Zweiter Einrichtungsweg in der App:** „Mit Bridge“ (wie bisher) oder „Direkt mit FRITZ!Box (nur zu Hause)“.
2. **SIP-User-Agent in Swift** in `HousephoneKit/SIP/`, ohne Fremdbibliothek:
   - RFC 3261 über UDP: REGISTER mit Erneuerung, INVITE in beide Richtungen, CANCEL, BYE, 486/603.
   - Transaktionen mit Timer A/B und E/F; Digest-Auth (MD5, `qop=auth`).
   - SDP mit PCMA und `telephone-event` (RFC 4733); RTP mit G.711 A-law, 20 ms.
   - G.711, Jitter-Puffer und Audio-Pfad (AVAudioEngine mit Voice Processing) kommen aus dem Watch-Pfad (ADR-0002).
   - Kein SRTP: Das Gespräch bleibt im eigenen WLAN, wie bei jedem DECT- oder IP-Telefon an der FRITZ!Box.
3. **Eingehende Anrufe:** Die App registriert sich, solange sie läuft.
   - Im Hintergrund übernimmt das die Extension `HousephoneLocalPush` (Local Push Connectivity).
   - **Die Extension steht hinter einem Feature-Flag.** Das Target ist im Xcode-Projekt angelegt, wird aber standardmäßig **nicht eingebettet**. Sonst bräche jeder signierte Build, solange Apple das Entitlement nicht freigegeben hat. CI baut sie separat.
   - Freischalten: Entitlement beantragen → Profile anlegen → in Xcode die Extension `HousephoneLocalPush` am App-Target einbetten (*General → Frameworks, Libraries, and Embedded Content*) und `HOUSEPHONE_LOCAL_PUSH` setzen.
4. **Heim-WLAN per SSID-Eingabe.** Die SSID automatisch auszulesen verlangte das Entitlement „Access WiFi Information“ und die Standortfreigabe. Das ist für eine Telefon-App unverhältnismäßig.
5. **TR-064 direkt:** Telefonbuch und Anrufliste lädt die App im Direktmodus selbst von der FRITZ!Box (`TR064Client`: TLS auf dem Security-Port, Digest-Auth). Die Abbildung ist die der Bridge, in Swift nachgebaut. Die Funktion ist optional; ohne TR-064-Zugang telefoniert die App trotzdem.
6. **Apple Watch ist im Direktmodus aus.** Die Watch spricht nur mit der Bridge (ADR-0002). Die Einstellungen sagen das offen.
7. **Zugangsdaten** (SIP-Passwort, TR-064-Passwort) liegen nur im Schlüsselbund, nie in `UserDefaults` oder Logs.

## Grenzen (werden in der App klar angezeigt)

- **Nur im Heim-WLAN.** Unterwegs ist man nicht erreichbar und kann nicht anrufen („Unterwegs nicht erreichbar“).
- Ohne freigegebene Local-Push-Extension klingelt das iPhone **nur, solange die App im Vordergrund läuft**.
- Keine Apple Watch.
- Nur G.711 A-law (kein G.722/HD).
- Die FRITZ!Box muss ein IP-Telefon mit Benutzername und Passwort für Housephone haben (Telefonie → Telefoniegeräte → Neues Gerät → „Telefon (mit und ohne Anrufbeantworter)“ → LAN/WLAN (IP-Telefon)).

## Offene Punkte

- **Übergabe eines Anrufs von der Extension an die App.** Die Extension registriert sich und meldet eingehende Anrufe per `reportIncomingCall`. Den klingelnden SIP-Dialog hält aber sie, nicht der User-Agent der App. Geplant ist, dass die Extension als SIP-Relay für die App dient (App ↔ Extension über localhost, Extension ↔ FRITZ!Box über UDP) und RTP direkt aus der App läuft. Das ist noch nicht gebaut.
- **Passwort für die Extension:** Sie liest den Schlüsselbund-Eintrag der App über eine gemeinsame Access Group (`keychainGroup` in der Provider-Konfiguration). Die Access Group wird beim Freischalten eingerichtet.
- **Nicht am echten Gerät getestet:** Registrierung, Anrufe und TR-064 gegen eine echte FRITZ!Box. Die Unit-Tests spielen die FRITZ!Box nach.

## Folgen

- Zwei Anrufpfade in der App; `CallCenter` bleibt für die Bridge, der Direktmodus hat eine eigene `DirectCallEngine` hinter derselben CallKit-Schicht.
- Der SIP-Stack ist Eigenbau und damit Wartungslast. Deshalb deckt er bewusst nur das ab, was die FRITZ!Box braucht (UDP, kein TCP/TLS, kein Re-INVITE außer Beantworten mit der aktuellen SDP, kein Hold).
- Ein späterer App-Store-Release braucht für den Direktmodus mit Hintergrund-Klingeln die Apple-Freigabe; ohne sie bleibt nur Vordergrund.
