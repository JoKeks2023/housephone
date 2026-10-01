# Veröffentlichung – was vor einem App-Store-Release zu klären ist

**Stand:** 2026-09-29. Der Nutzer will das Projekt „irgendwann“ im App Store veröffentlichen. Heute ist es für den Eigengebrauch gebaut: eigene Bridge, eigener APNs-Key, Build aus Xcode.

## 1. Push für fremde Bridges (Blocker)

**Problem:** VoIP-Pushes an eine App-Store-App kann nur senden, wer den **APNs-Key des App-Anbieters** besitzt. Heute schickt jede Bridge die Pushes selbst mit diesem Key. Bei einer veröffentlichten App betreibt aber jeder Nutzer seine **eigene** Bridge, und der Anbieter kann seinen Key nicht an alle verteilen.

**Vorschlag: Push-Relay des Anbieters**
- **Ablauf:** Die Bridge schickt einen Push-Auftrag an einen kleinen Relay-Dienst des Anbieters, zum Beispiel einen Cloudflare Worker. Der Relay hält den APNs-Key und sendet an Apple weiter.
- **Authentifizierung:** Jede Bridge hat ein eigenes Token, dazu kommen Rate-Limits pro Bridge und pro Gerät.
- **Datenschutz:** Die Nutzlast (Nummer, Name) verschlüsselt die Bridge Ende-zu-Ende mit einem Geräteschlüssel aus der Kopplung. Der Relay sieht nur Push-Token und Zeitpunkt. Die App entschlüsselt im PushKit-Handler, bevor sie den Anruf an CallKit meldet.
- **Noch zu prüfen:** ob Cloudflare Workers APNs (HTTP/2) direkt ansprechen können; sonst ein kleiner eigener Dienst.

## 2. Lizenz und Repository

- **Heutiger Stand:** Das Repository steht unter **GPL-3.0**, weil es aus *Telephone* (64characters) entstanden ist. Die neuen Teile (`bridge/`, `ios/`) enthalten keinen Telephone-Code.
- **GPL und App Store:** Die GPL gilt als schwer vereinbar mit den Nutzungsbedingungen des App Stores.
- **Vor einem Release:**
  - Housephone in ein **eigenes Repository ohne Telephone-Historie** überführen oder die neuen Teile ausdrücklich eigenständig lizenzieren.
  - Das ist eine rechtliche Entscheidung des Nutzers.
- **Abhängigkeiten:**
  - ✅ **Danksagungen in der App** (Einstellungen → Über → Danksagungen, 2026-09-29):
    - Die iPhone-App liefert nur über WebRTC (stasel/WebRTC 153.0.0) fremden Code aus.
    - Welche Komponenten darin stecken, ist an der Binary belegt: Pfade und Versionskennungen in den Strings.
    - Nachgewiesen: WebRTC, BoringSSL, libSRTP, Opus, Abseil, Protocol Buffers, LLVM libc++abi, libvpx (+ PATENTS), libaom, dav1d, libyuv.
    - Die Lizenztexte liegen wörtlich aus den offiziellen Quellen unter `ios/Housephone/Resources/Licenses/`. Die Datei zu libaom stammt aus dem Release v3.15.1.
  - **Offen:** Die Volltexte von **libyuv** und der **WebRTC-PATENTS** gibt es nur bei googlesource, das am 2026-09-29 durchgehend mit 503 antwortete. Beide sind vorerst in der App verlinkt; vor einem Release einbetten.
  - Die Watch-App und HousephoneKit nutzen keine Fremdbibliotheken.
  - Die Bridge wird nicht mit der App ausgeliefert. pion (MIT) und diago/sipgo (MPL-2.0 bzw. BSD-2) betreffen ihre eigene Verteilung. MPL heißt: geänderte Dateien offenlegen, sonst keine Pflichten.

## 3. Marke

„FRITZ!Box“ ist eine Marke der FRITZ! GmbH (ehemals AVM). Sie gehört nicht in den App-Namen oder das Icon. Beschreibend darf sie verwendet werden, etwa „Telefonieren über deine FRITZ!Box“.

## 4. App Review

- **Zugang für die Prüfer:** Die Prüfer haben keine FRITZ!Box. Nötig ist ein **Demo-Zugang**: eine Demo-Bridge des Anbieters mit Testanschluss oder ein Demo-Modus in der App.
- **Begründung für VoIP-Hintergrund und PushKit:** Echte VoIP-Anrufe über CallKit sind vorhanden. In den Review-Notizen den Ablauf erklären.
- **Datenschutz:**
  - ✅ **Privacy Manifest** in iPhone- und Watch-App (`ios/Housephone/Resources/PrivacyInfo.xcprivacy`, `ios/HousephoneWatch/Resources/PrivacyInfo.xcprivacy`; die CI prüft, dass beide in den Bundles liegen):
    - **Kein Tracking.**
    - **`NSPrivacyCollectedDataTypes` ist leer.** Die Apps senden Daten (Nummern, Namen, Push-Token, Ton) nur an die **eigene Bridge des Nutzers**; der Entwickler betreibt sie nicht und hat keinen Zugriff. Nach Apples Definition ist das keine Datenerhebung durch den Entwickler.
    - **Required-Reason-API:** Nur UserDefaults mit Grund **CA92.1** (nur für die App selbst lesbar). Das iPhone nutzt es über `@AppStorage` für Ansichtseinstellungen, die Watch für Kopplungsdatum und letzte Anrufe. Datei-Zeitstempel, Systemstartzeit, Speicherplatz-APIs und aktive Tastaturen verwendet der eigene Code nicht; das ist per Code-Suche geprüft.
    - **WebRTC** bringt ein eigenes Manifest mit (Systemstartzeit 35F9.1/8FFB.1, Datei-Zeitstempel C617.1).
  - **Muss nachgezogen werden, sobald es das Push-Relay gibt:** Dann erhebt der Anbieter Push-Tokens und Gerätekennungen. Manifest (`NSPrivacyCollectedDataTypeDeviceID`, Zweck App-Funktion) und App-Datenschutzangaben anpassen.
  - Nutzungsbeschreibungen für Mikrofon, Kamera und Kontakte sind vorhanden.
- **Verschlüsselung/Export:**
  - Die App nutzt Standardverschlüsselung (TLS, DTLS-SRTP).
  - In App Store Connect muss die Exportfrage beantwortet werden; rechtlich prüfen, ob die Ausnahme für Standardverschlüsselung greift.
  - **Achtung:** In den `Info.plist`-Dateien von iPhone und Watch steht bereits `ITSAppUsesNonExemptEncryption: false`, für iPhone und Watch. Das ist eine Erklärung gegenüber Apple und sollte vor einem Release bewusst bestätigt oder geändert werden.

## 5. Einrichtung für normale Nutzer

Die heutige Einrichtung (Docker, Cloudflare Tunnel, APNs-Key, Portfreigabe) ist für Entwickler gebaut. Für eine Veröffentlichung:

- **Die Bridge richtet das IP-Telefon selbst in der FRITZ!Box ein** (TR-064 `X_VoIP`).
- **Fertige Pakete:** Docker-Einzeiler, Home-Assistant-Add-on, Synology/Unraid.
- **Kein eigener APNs-Key:** Mit dem Push-Relay aus Abschnitt 1 entfällt er.
- **Erreichbarkeit ohne eigene Domain:** z. B. ein Tunnel-Dienst des Anbieters oder eine geführte Einrichtung.

## 6. Sonstiges

- App-Store-Material: Screenshots, Beschreibung Deutsch und Englisch, Support-URL, Datenschutzerklärung
- Barrierefreiheits-Audit auf echten Geräten
- Fehlerberichte/Absturzberichte, ohne personenbezogene Daten
