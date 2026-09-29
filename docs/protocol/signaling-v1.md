# Housephone Signalisierung v1

Vertrag zwischen **Bridge** (`bridge/`, Go) und **Geräten** (`ios/`: iPhone-App und Watch-App).
Änderungen an diesem Dokument müssen in beiden Implementierungen nachgezogen werden.

## Transport

- WebSocket: `GET /v1/ws`, von außen immer TLS (`wss://`), z. B. über Cloudflare Tunnel.
- Health-Check: `GET /v1/health` → `200 {"status":"ok","version":"<bridge-version>","sipRegistered":true}`.
- Jede WebSocket-Textnachricht ist genau **ein** JSON-Objekt (UTF-8). Binärnachrichten gibt es nur für Ton im Medienweg `websocket-pcma` (siehe „Erweiterung v1.1“).
- Keepalive: WebSocket-Ping/Pong auf Protokollebene. Die Bridge sendet alle 20 s einen Ping; ohne Pong nach 20 s schließt sie die Verbindung.
- Die erste Nachricht (`pair` bzw. `hello`) muss innerhalb von **10 s** nach dem Upgrade kommen, sonst schließt die Bridge (Close-Code `1008`).

### Authentifizierung

| Verbindungsart | Header beim Upgrade | Erste Nachricht |
|---|---|---|
| Kopplung | keiner | `pair` |
| Normal | `Authorization: Bearer <deviceId>.<deviceSecret>` | `hello` |

- `deviceId`: UUID (klein geschrieben, mit Bindestrichen).
- `deviceSecret`: 32 Zufallsbytes, base64url ohne Padding (43 Zeichen).
- Die Bridge speichert nur `sha256(deviceSecret)` und vergleicht in konstanter Zeit.
- Bei ungültigem Token antwortet die Bridge mit HTTP `401`; es findet kein Upgrade statt.
- Ohne Header ist nur `pair` erlaubt. Jede andere Nachricht → `error{code:"unauthorized"}`, danach wird die Verbindung geschlossen.
- Pro Gerät ist höchstens **eine** Verbindung aktiv. Eine neue Verbindung desselben Geräts schließt die alte mit Close-Code `4001` („replaced“).

## Nachrichtenformat

```json
{ "type": "call.offer", "payload": { ... } }
```

- `type`: String, siehe Tabellen unten. Unbekannte Typen werden ignoriert (Vorwärtskompatibilität).
- `payload`: Objekt, immer vorhanden (ggf. `{}`). Unbekannte Felder werden ignoriert.
- Feldnamen: camelCase.
- Zeitstempel: RFC 3339 in UTC **ohne Sekundenbruchteile**, z. B. `2026-09-29T18:04:05Z`.
- `callId`: UUID, klein geschrieben. Wird bei **eingehenden** Anrufen von der Bridge erzeugt und bei **ausgehenden** vom Gerät (CallKit-UUID). Das Gerät verwendet die `callId` direkt als CallKit-UUID.
  - Die Bridge akzeptiert auch groß geschriebene UUIDs (Swift `UUID().uuidString`) und normalisiert sie; **alle Antworten tragen die klein geschriebene Form**. Geräte vergleichen `callId`s daher über `UUID`, nicht als String.

## Gerät → Bridge

| type | payload | Wann |
|---|---|---|
| `pair` | `{code, deviceName, platform, model?}` | Erste Nachricht einer Kopplungsverbindung. `platform`: `"ios"` \| `"watchos"`. |
| `hello` | `{appVersion, platform, pushToken?, pushEnvironment?}` | Erste Nachricht jeder normalen Verbindung. `pushToken`: VoIP-Token als Hex-String (klein). `pushEnvironment`: `"development"` \| `"production"`. |
| `device.update` | `{pushToken?, pushEnvironment?, deviceName?}` | Wenn sich z. B. das Push-Token ändert. Ungültige Werte (Token nicht klein geschriebenes Hex) werden ignoriert. |
| `device.unpair` | `{}` | Gerät entkoppelt sich selbst. Die Bridge löscht das Gerät samt Push-Token und schließt die Verbindung mit Close-Code `1000`. |
| `call.attach` | `{callId}` | Gerät hängt sich an einen Anruf – nach einem VoIP-Push, nach unaufgefordertem `call.incoming` und nach jedem Wiederverbinden. **Idempotent** und in jeder aktiven Phase erlaubt (klingelnd, Early Media, verbunden), siehe „Anhängen“. |
| `call.dial` | `{callId, number}` | Ausgehender Anruf. `number`: gewählte Ziffern, erlaubt `0-9 * # +`. |
| `call.answer` | `{callId, sdp}` | WebRTC-SDP-Answer auf ein `call.offer` (vollständige Kandidaten, kein Trickle-ICE). |
| `call.accept` | `{callId}` | Nutzer hat einen **eingehenden** Anruf angenommen. |
| `call.hangup` | `{callId, reason?}` | Nutzer legt auf oder lehnt ab. `reason`: `"hangup"` \| `"declined"` \| `"failed"`. |
| `call.dtmf` | `{callId, digits}` | Tastentöne während des Gesprächs, `digits` aus `0-9 * #`. |

## Bridge → Gerät

| type | payload | Wann |
|---|---|---|
| `pair.ok` | `{deviceId, deviceSecret, bridgeId, bridgeName}` | Kopplung erfolgreich. Danach schließt die Bridge die Kopplungsverbindung (Close-Code `1000`). |
| `welcome` | `{bridgeId, bridgeName, bridgeVersion, sipRegistered}` | Antwort auf `hello`. |
| `status` | `{sipRegistered}` | Wenn sich der Registrierungsstatus an der FRITZ!Box ändert. |
| `call.incoming` | `{callId, caller, callerName?, startedAt}` | Antwort auf `call.attach`, **nur solange der Anruf klingelt**. Wird außerdem unaufgefordert an bereits verbundene Geräte gesendet – das ist nur eine Benachrichtigung; ein `call.offer` kommt erst nach `call.attach`. |
| `call.offer` | `{callId, sdp, iceServers}` | WebRTC-SDP-Offer. Die Bridge ist **immer** der Offerer. `iceServers`: `[{urls:[String], username?, credential?}]`, darf leer sein. Kann während eines laufenden Anrufs **erneut** kommen (ICE-Restart nach Re-Attach oder ICE-Fehler) – das Gerät antwortet jedes Mal mit `call.answer` auf derselben PeerConnection. |
| `call.state` | `{callId, state}` | `state`: `"ringing"` (Gegenstelle klingelt, nur ausgehend), `"early_media"` (Gegenstelle sendet Ton vor Annahme), `"connected"`. |
| `call.ended` | `{callId, reason, sipCode?}` | Anruf ist für dieses Gerät beendet. Siehe Gründe unten. |
| `error` | `{code, message, callId?}` | Fehler, siehe Codes unten. |

### Gründe in `call.ended`

| reason | Bedeutung | CallKit-Mapping |
|---|---|---|
| `remote_hangup` | Gegenstelle hat aufgelegt | `.remoteEnded` |
| `remote_cancelled` | Anrufer hat vor Annahme aufgelegt | `.unanswered` |
| `answered_elsewhere` | Anderes Gerät bzw. anderes Telefon an der FRITZ!Box hat angenommen | `.answeredElsewhere` |
| `declined_elsewhere` | Auf einem anderen Gerät abgelehnt | `.declinedElsewhere` |
| `busy` | Ziel besetzt (SIP 486/600) | `.failed` |
| `rejected` | Ziel lehnt ab (SIP 603) | `.failed` |
| `not_found` | `callId` unbekannt (auch nicht in den letzten 2 Minuten beendet) | `.failed` (eingehend: `.unanswered`) |
| `failed` | Technischer Fehler (SIP 4xx/5xx, ICE-Fehler, Timeout) | `.failed` |
| `local_hangup` | Bestätigung eines `call.hangup` | – (bereits lokal beendet) |

### Fehlercodes in `error`

`unauthorized`, `bad_request`, `pairing_invalid` (Code falsch, abgelaufen oder bereits benutzt), `pairing_rate_limited`, `sip_unavailable` (nicht an der FRITZ!Box registriert), `call_not_found`, `invalid_number`, `internal`.

- `call.answer` und `call.dtmf` für unbekannte Anrufe → `error{call_not_found}`.
- `call.attach`, `call.accept` und `call.hangup` für unbekannte Anrufe → `call.ended` (siehe „Späte Nachrichten“).

## Abläufe

### Kopplung

1. Auf dem Server: `housephone-bridge pair -name "iPhone Joris"` gibt einen QR-Code und einen Link aus:
   `housephone://pair?url=<urlencoded wss-URL>&code=<code>&name=<urlencoded bridgeName>`
   - Reihenfolge der Parameter beliebig; Leerzeichen als `%20`. Die Bridge erzeugt z. B.
     `housephone://pair?code=K7P2XH9QRM&name=Zuhause&url=wss%3A%2F%2Fphone.example.com%2Fv1%2Fws`.
   - `code`: 10 Zeichen aus `A-Z2-9` ohne `0 O 1 I`.
   - Gültig **10 Minuten**, einmalig verwendbar.
   - Codes liegen in der Datei `pairing.json` im Datenverzeichnis, damit CLI und laufender Dienst sie teilen.
2. Die App scannt den QR-Code oder öffnet den Link, verbindet sich ohne Auth-Header und sendet `pair`.
3. Die Bridge antwortet mit `pair.ok` und schließt die Verbindung. Die App speichert `deviceId`, `deviceSecret` und `url` im Schlüsselbund.
   - Als Gerätename speichert die Bridge den Namen aus `pair -name`, sonst `deviceName` aus der `pair`-Nachricht.
4. Rate-Limit: höchstens 5 fehlgeschlagene `pair`-Versuche pro Minute und Quell-IP → `pairing_rate_limited`. Als Quell-IP gilt bei Cloudflare der Header `CF-Connecting-IP`.

### Anhängen (`call.attach`)

- Das Gerät sendet **immer** `call.attach`, auch wenn es `call.incoming` schon ohne Push bekommen hat.
- Die Bridge antwortet auf jedes `call.attach` mit `call.incoming` (nur solange es klingelt) und einem `call.offer`; in verbundenen bzw. ausgehenden Anrufen zusätzlich mit dem aktuellen `call.state`.
- Pro Gerät und Anruf gibt es **genau eine** PeerConnection. Ein wiederholtes `call.attach` – auch mehrfach in derselben WebSocket-Verbindung – erzeugt nie eine zweite:
  - Offer wird gerade erzeugt → es geht an die aktuelle Verbindung, sobald es fertig ist.
  - Offer gesendet, aber noch nicht beantwortet → **dasselbe** Offer wird unverändert erneut gesendet (gleiche ICE-Zugangsdaten, jede der beiden Answers passt).
  - Offer beantwortet → neues Offer von **derselben** PeerConnection mit ICE-Restart (gleicher DTLS-Fingerprint). Das Gerät behandelt ein weiteres Offer zum selben Anruf als ICE-Restart seiner bestehenden Verbindung.
- Eine `call.answer` auf ein bereits beantwortetes Offer (Duplikat) ignoriert die Bridge ohne Fehler.
- Verliert ein **klingelndes** Gerät die WebSocket-Verbindung, behält die Bridge dessen PeerConnection; beim nächsten `call.attach` folgt ein ICE-Restart-Offer.

### Eingehender Anruf

```
FRITZ!Box        Bridge                         Gerät (App)
   │ INVITE (SDP) ─►│
   │◄─ 100 Trying ──│
   │◄─ 180 Ringing ─│── VoIP-Push {callId, caller, callerName} ──► (APNs)
   │                │                              │ reportNewIncomingCall (sofort!)
   │                │◄──── WSS connect + hello ────│
   │                │◄──── call.attach{callId} ────│
   │                │── call.incoming ────────────►│
   │                │── call.offer{sdp} ──────────►│
   │                │◄──── call.answer{sdp} ───────│   ICE/DTLS verbinden sich schon beim Klingeln
   │                │◄──── call.accept ────────────│   Nutzer nimmt an (CXAnswerCallAction)
   │◄─ 200 OK (SDP)─│── call.ended{answered_elsewhere} ─► andere Geräte
   │── ACK ────────►│── call.state{connected} ────►│
   │◄════ RTP ═════►│◄═══════ SRTP (WebRTC) ══════►│
```

- Codec-Wahl: erster Codec aus `[G722, PCMA, PCMU]`, den die FRITZ!Box im INVITE anbietet. Das `call.offer` enthält **nur** diesen Codec. Die `200 OK` an die FRITZ!Box enthält den Codec des **annehmenden** Geräts – bei WebRTC-Geräten ist das dieser Codec, bei `websocket-pcma`-Geräten PCMA (v1.1).
- Mehrere Geräte: Die Bridge pusht alle gekoppelten Geräte mit Push-Token. Das erste `call.accept` gewinnt, alle anderen bekommen `call.ended{answered_elsewhere}`.
- Ablehnen (`call.hangup{reason:"declined"}`) beendet den Anruf nur für dieses Gerät. Haben **alle** angehängten Geräte abgelehnt und ist kein weiteres mehr ausstehend, antwortet die Bridge der FRITZ!Box mit `486 Busy Here`. Die FRITZ!Box lässt dann ggf. andere Telefone weiterklingeln.
- Ist **kein** Gerät erreichbar (keins online, keins mit Push-Token, oder alle Pushes schlugen fehl), antwortet die Bridge mit `480 Temporarily Unavailable` und sendet kein `180 Ringing`.
- Nur Anrufe von der IP des Registrars (FRITZ!Box) werden angenommen, andere INVITEs bekommen `403`. Enthält das Angebot keinen der drei Codecs → `488`.
- CANCEL von der FRITZ!Box → `call.ended{remote_cancelled}`. Enthält der CANCEL `Reason: SIP;cause=200`, wird stattdessen `answered_elsewhere` gesendet (anderes Telefon an der FRITZ!Box hat angenommen).
- Kommt `call.attach` für einen unbekannten oder beendeten Anruf → `call.ended` (siehe „Späte Nachrichten“).
- Das Gerät beendet den CallKit-Anruf mit `.failed`, wenn es sich nicht innerhalb von **10 s** nach dem Push verbinden und anhängen konnte.

### Ausgehender Anruf

```
Gerät                           Bridge                        FRITZ!Box
  │── call.dial{callId,number} ─►│
  │◄─ call.offer{sdp, [G722,PCMA,PCMU]} ─│
  │── call.answer{sdp} ─────────►│  Codec = erster Codec, den das Gerät in der Answer wählt
  │                              │── INVITE (nur dieser Codec) ─►│
  │◄─ call.state{ringing} ───────│◄─ 180 Ringing ────────────────│
  │◄─ call.state{early_media} ───│◄─ 183 Session Progress (SDP) ─│   Ton wird ab jetzt durchgereicht
  │◄─ call.state{connected} ─────│◄─ 200 OK ─────────────────────│── ACK ─►
  │◄═══════ SRTP ═══════════════►│◄════════════ RTP ════════════►│
```

- Bei `call.state{ringing}` **ohne** vorheriges `early_media` spielt die App selbst einen Freiton ab, bis `early_media` oder `connected` kommt.
- Antwortet die FRITZ!Box mit `488 Not Acceptable Here`, endet der Anruf mit `call.ended{failed, sipCode:488}`. v1 versucht **keinen** zweiten Codec (dafür müsste das WebRTC-Offer neu verhandelt werden) – das kommt in v2.
- Fehlerantworten der FRITZ!Box werden gemappt: 486/600 → `busy`, 603 → `rejected`, 404/484 → `invalid_number`-Fehler plus `call.ended{failed}`, sonst `failed` mit `sipCode`.
- Eine ungültige `number` (nicht `^\+?[0-9*#]{1,32}$`) → `error{invalid_number}` plus `call.ended{failed}`, ohne dass gewählt wird.
- Ist die Bridge nicht an der FRITZ!Box registriert → `error{sip_unavailable, callId}` plus `call.ended{failed}`.

### Auflegen

- Gerät: `call.hangup` → Bridge sendet BYE (verbunden) bzw. CANCEL (ausgehend, noch nicht verbunden) und bestätigt mit `call.ended{local_hangup}`.
- FRITZ!Box: BYE → `call.ended{remote_hangup}`.
- Bricht die WebSocket-Verbindung eines **verbundenen** Anrufs ab, hält die Bridge den Anruf **30 s** offen. So lange darf sich das Gerät mit `call.attach` erneut anhängen (Netzwechsel). Danach legt die Bridge auf.
  - Beim Wieder-Anhängen sendet die Bridge ein neues `call.offer` (ICE-Restart auf derselben PeerConnection, siehe „Anhängen“) und den aktuellen `call.state`. Das Gerät antwortet mit `call.answer`.
- Meldet die PeerConnection des aktiven Geräts einen ICE-Fehler, während die WebSocket-Verbindung steht, sendet die Bridge von sich aus bis zu 3-mal ein neues `call.offer` mit ICE-Restart. Erholt sich die Verbindung nicht innerhalb von 30 s, legt sie auf.

### Späte Nachrichten

Die Bridge merkt sich beendete Anrufe **2 Minuten** lang. Ein Gerät, das sich danach erst anhängt (z. B. Push kam spät), bekommt `call.ended` mit dem **echten** Grund (`answered_elsewhere`, `remote_cancelled`, `declined_elsewhere` …) statt `not_found`. Erst danach gilt `not_found`.

## VoIP-Push (APNs)

- Topic: `<bundleId>.voip` (App: `com.jorisconrad.housephone.voip`)
- `apns-push-type: voip`, `apns-priority: 10`, `apns-expiration: 0`, `apns-collapse-id: <callId>`
- Payload:

```json
{ "type": "incoming_call", "v": 1, "callId": "…", "caller": "+4930123456", "callerName": "Oma", "bridgeId": "…" }
```

- `caller`: Nummer aus dem SIP-`From` (User-Teil). Leer, wenn unterdrückt.
- `callerName`: Anzeigename aus dem SIP-`From`, falls vorhanden.
- Antwortet APNs mit `410 Unregistered`, `400 BadDeviceToken` oder `400 DeviceTokenNotForTopic`, löscht die Bridge das Push-Token dieses Geräts.
- `caller`/`callerName` sind leer, wenn die FRITZ!Box `anonymous`/`unknown` meldet; ein Anzeigename gleich der Nummer entfällt.
- Das Gerät **muss** jeden VoIP-Push sofort per CallKit melden (Pflicht seit iOS 13). Auch dann, wenn der Anruf danach als `not_found` endet.

## Medien (WebRTC)

- Die Bridge ist immer der Offerer. Audio läuft als `sendrecv`, genau ein Audio-Track, kein Video, kein DataChannel.
- Die Bridge lauscht mit ICE-UDP-Mux auf **einem** UDP-Port (Standard `50000`). Als Kandidaten meldet sie ihre LAN-IPs (`typ host`) und die öffentliche IPv4 als zusätzlichen `typ srflx`-Kandidaten auf demselben Port.
- Ausgehend bietet die Bridge `G722, PCMA, PCMU` an; der **erste Audio-Codec in der Answer** des Geräts bestimmt, was beide Seiten senden. Eingehend enthält das Offer nur den Codec der FRITZ!Box-Seite.
- Kein Trickle-ICE in v1: SDP wird erst gesendet, wenn das ICE-Gathering abgeschlossen ist. Das Gerät wartet dafür höchstens 2 s.
- DTMF läuft nicht über RTP, sondern über `call.dtmf`. Die Bridge sendet RFC 4733 an die FRITZ!Box.

## Erweiterung v1.1: Apple Watch (ADR-0002)

Diese Erweiterung ist rückwärtskompatibel. Ein Gerät ohne die neuen Felder verhält sich wie in v1 (WebRTC).

### Geräte-Fähigkeiten und Push-Topic

`hello` und `device.update` bekommen zwei optionale Felder:

| Feld | Werte | Standard |
|---|---|---|
| `mediaCapabilities` | Array aus `"webrtc"`, `"websocket-pcma"` | `["webrtc"]` |
| `pushTopic` | APNs-Topic dieses Geräts, z. B. `com.jorisconrad.housephone.watchkitapp.voip` | `apns.topic` aus der Bridge-Konfiguration |

- Die Bridge speichert beide Werte pro Gerät.
- `pushTopic` muss mit dem Bundle-Präfix der Bridge beginnen, also mit `apns.topic` ohne das Suffix `.voip`. Andernfalls antwortet die Bridge mit `error{bad_request}`.
- Die Watch meldet `["websocket-pcma"]`.

### HTTPS-Endpunkte (für die Watch)

Auf watchOS darf eine App WebSocket nur während eines CallKit-Anrufs öffnen. Deshalb gibt es für Kopplung, Push-Token und den Anrufstatus beim Klingeln zusätzlich reines HTTPS unter demselben Host wie `/v1/ws`:

| Methode + Pfad | Auth | Body | Antwort |
|---|---|---|---|
| `POST /v1/pair` | keine | Payload von `pair` | `200` + Payload von `pair.ok` bzw. `4xx` + Payload von `error` |
| `PUT /v1/device` | `Authorization: Bearer <deviceId>.<deviceSecret>` | Payload von `device.update` (inkl. `mediaCapabilities`, `pushTopic`) | `204` bzw. `401`/`400` + `error` |
| `DELETE /v1/device` | Bearer | – | `204`; wirkt wie `device.unpair` |
| `GET /v1/calls/{callId}` | Bearer | – | `200 {callId, state, reason?, sipCode?}` bzw. `404` + `error{call_not_found}` |

- Rate-Limit und Code-Regeln sind dieselben wie bei `pair` über WebSocket.
- Fehlerantworten: `400 bad_request`, `401 unauthorized`, `403 pairing_invalid`, `429 pairing_rate_limited`, `404 call_not_found`.

#### Anrufstatus (`GET /v1/calls/{callId}`)

Eine klingelnde Watch hat evtl. noch keine WebSocket-Verbindung und erfährt so nichts von CANCEL oder einer Annahme an anderer Stelle. Deshalb fragt sie den Status des Anrufs **aus ihrer eigenen Sicht** ab:

| `state` | Bedeutung |
|---|---|
| `ringing` | Eingehend: klingelt noch. Ausgehend: wird gewählt bzw. die Gegenstelle klingelt. |
| `connected` | Dieses Gerät führt das Gespräch (eingehend: nach `call.accept` dieses Geräts). |
| `ended` | Für dieses Gerät beendet. `reason`/`sipCode` wie in `call.ended`, z. B. `answered_elsewhere`, wenn ein anderes Gerät angenommen hat, `remote_cancelled` nach CANCEL, `local_hangup` nach eigenem Ablehnen. |

- `404 call_not_found`:
  - wenn der Anruf unbekannt ist,
  - wenn dieses Gerät nicht beteiligt war (weder gepusht noch informiert, angehängt oder wählend),
  - oder wenn das Ende länger als 2 Minuten zurückliegt (Tombstone abgelaufen).
- Poll-Empfehlung:
  - Alle **2 s** abfragen, und nur solange das Gerät klingelt und keine WebSocket-Verbindung zu diesem Anruf hat.
  - Netzfehler werden ignoriert, danach weiter pollen.
  - Nach Annahme bzw. `call.attach` aufhören.

### Kopplung der Watch über das iPhone

| type | Richtung | payload |
|---|---|---|
| `pair.companion.request` | Gerät → Bridge | `{deviceName, platform}`, `platform` ist in der Regel `"watchos"` |
| `pair.companion` | Bridge → Gerät | `{code, url, expiresAt}` |

- Nur ein bereits gekoppeltes Gerät darf einen Code anfordern.
- Der Code folgt denselben Regeln wie `housephone-bridge pair`: 10 Minuten gültig, einmalig. `deviceName` wird wie `pair -name` behandelt.
- Das iPhone gibt `{url, code}` per WatchConnectivity an die Watch weiter. Die Watch koppelt sich per `POST /v1/pair`.

### Medien über WebSocket (`websocket-pcma`)

Für Geräte mit `mediaCapabilities: ["websocket-pcma"]` gilt:

- Nach `call.attach` bzw. `call.dial` sendet die Bridge **statt `call.offer`**:

  | type | payload |
  |---|---|
  | `call.media` | `{callId, transport:"websocket", codec:"PCMA", sampleRate:8000, frameMs:20}` |

  Ein `call.answer` gibt es für diese Geräte nicht.
- **Audio läuft als binäre WebSocket-Nachricht** in beide Richtungen, auf derselben Verbindung:
  - `byte 0` = `0x01` (Audio), danach genau 160 Byte A-law, also 20 ms bei 8 kHz.
  - Andere Typ-Bytes sind reserviert und werden ignoriert.
  - Pro Verbindung ist höchstens ein Anruf mit Ton aktiv.
- **Wann Ton fließt:**
  - Bridge → Gerät: ab Early Media bzw. `connected`.
  - Gerät → Bridge: sobald das Gerät angenommen hat (eingehend) bzw. nach `call.media` (ausgehend). Die Bridge verwirft Ton, solange der Anruf nicht verbunden ist.
- **Keine Codec-Wandlung:**
  - Eingehend beantwortet die Bridge das INVITE der FRITZ!Box beim `call.accept` mit dem Codec des annehmenden Geräts (WebRTC: laut Answer, `websocket-pcma`: PCMA).
  - Ausgehend von einem `websocket-pcma`-Gerät bietet die INVITE nur PCMA an.
- DTMF weiterhin über `call.dtmf`.
- Bricht die Verbindung ab, gilt dieselbe 30-s-Reattach-Regel. Nach erneutem `call.attach` sendet die Bridge wieder `call.media`, und der Ton geht auf der neuen Verbindung weiter.
