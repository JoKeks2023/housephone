# Housephone Signalisierung v1

Vertrag zwischen **Bridge** (`bridge/`, Go) und **Geräten** (`ios/`, später watchOS).
Änderungen an diesem Dokument müssen in beiden Implementierungen nachgezogen werden.

## Transport

- WebSocket: `GET /v1/ws`, von außen immer TLS (`wss://`), z. B. über Cloudflare Tunnel.
- Health-Check: `GET /v1/health` → `200 {"status":"ok","version":"<bridge-version>","sipRegistered":true}`.
- Jede WebSocket-Textnachricht ist genau **ein** JSON-Objekt (UTF-8). Binärnachrichten werden nicht verwendet.
- Keepalive: WebSocket-Ping/Pong auf Protokollebene. Die Bridge sendet alle 20 s einen Ping; ohne Pong nach 20 s schließt sie die Verbindung.

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
- Zeitstempel: ISO-8601 in UTC.
- `callId`: UUID, klein geschrieben. Wird bei **eingehenden** Anrufen von der Bridge erzeugt und bei **ausgehenden** vom Gerät (CallKit-UUID). Das Gerät verwendet die `callId` direkt als CallKit-UUID.

## Gerät → Bridge

| type | payload | Wann |
|---|---|---|
| `pair` | `{code, deviceName, platform, model?}` | Erste Nachricht einer Kopplungsverbindung. `platform`: `"ios"` \| `"watchos"`. |
| `hello` | `{appVersion, platform, pushToken?, pushEnvironment?}` | Erste Nachricht jeder normalen Verbindung. `pushToken`: VoIP-Token als Hex-String (klein). `pushEnvironment`: `"development"` \| `"production"`. |
| `device.update` | `{pushToken?, pushEnvironment?, deviceName?}` | Wenn sich z. B. das Push-Token ändert. |
| `call.attach` | `{callId}` | Nach einem VoIP-Push: Gerät hängt sich an den eingehenden Anruf. |
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
| `call.incoming` | `{callId, caller, callerName?, startedAt}` | Antwort auf `call.attach`, solange der Anruf noch klingelt. Wird auch ohne Push an verbundene Geräte gesendet (App im Vordergrund). |
| `call.offer` | `{callId, sdp, iceServers}` | WebRTC-SDP-Offer. Die Bridge ist **immer** der Offerer. `iceServers`: `[{urls:[String], username?, credential?}]`, darf leer sein. |
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
| `not_found` | `callId` unbekannt oder bereits beendet | `.failed` (eingehend: `.unanswered`) |
| `failed` | Technischer Fehler (SIP 4xx/5xx, ICE-Fehler, Timeout) | `.failed` |
| `local_hangup` | Bestätigung eines `call.hangup` | – (bereits lokal beendet) |

### Fehlercodes in `error`

`unauthorized`, `bad_request`, `pairing_invalid` (Code falsch, abgelaufen oder bereits benutzt), `pairing_rate_limited`, `sip_unavailable` (nicht an der FRITZ!Box registriert), `call_not_found`, `invalid_number`, `internal`.

## Abläufe

### Kopplung

1. Auf dem Server: `housephone-bridge pair --name "iPhone Joris"` gibt einen QR-Code und einen Link aus:
   `housephone://pair?url=<urlencoded wss-URL>&code=<code>&name=<urlencoded bridgeName>`
   - `code`: 10 Zeichen aus `A-Z2-9` ohne `0 O 1 I`.
   - Gültig **10 Minuten**, einmalig verwendbar.
   - Codes liegen in der Datei `pairing.json` im Datenverzeichnis, damit CLI und laufender Dienst sie teilen.
2. Die App scannt den QR-Code oder öffnet den Link, verbindet sich ohne Auth-Header und sendet `pair`.
3. Die Bridge antwortet mit `pair.ok` und schließt die Verbindung. Die App speichert `deviceId`, `deviceSecret` und `url` im Schlüsselbund.
4. Rate-Limit: höchstens 5 fehlgeschlagene `pair`-Versuche pro Minute und Quell-IP → `pairing_rate_limited`. Als Quell-IP gilt bei Cloudflare der Header `CF-Connecting-IP`.

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

- Codec-Wahl: erster Codec aus `[G722, PCMA, PCMU]`, den die FRITZ!Box im INVITE anbietet. Das `call.offer` enthält **nur** diesen Codec. Die `200 OK` an die FRITZ!Box enthält denselben Codec.
- Mehrere Geräte: Die Bridge pusht alle gekoppelten Geräte mit Push-Token. Das erste `call.accept` gewinnt, alle anderen bekommen `call.ended{answered_elsewhere}`.
- Ablehnen (`call.hangup{reason:"declined"}`) beendet den Anruf nur für dieses Gerät. Haben **alle** angehängten Geräte abgelehnt und ist kein weiteres mehr ausstehend, antwortet die Bridge der FRITZ!Box mit `486 Busy Here`. Die FRITZ!Box lässt dann ggf. andere Telefone weiterklingeln.
- CANCEL von der FRITZ!Box → `call.ended{remote_cancelled}`. Enthält der CANCEL `Reason: SIP;cause=200`, wird stattdessen `answered_elsewhere` gesendet (anderes Telefon an der FRITZ!Box hat angenommen).
- Kommt `call.attach` für einen unbekannten oder beendeten Anruf → `call.ended{not_found}`.
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
- Antwortet die FRITZ!Box mit `488 Not Acceptable Here` und war der Codec G.722, versucht die Bridge es einmal mit PCMA. Das WebRTC-Offer wird dabei **nicht** neu verhandelt; die Bridge beendet stattdessen mit `call.ended{failed, sipCode:488}`. Eine Neuverhandlung kommt erst in v2.
- Fehlerantworten der FRITZ!Box werden gemappt: 486/600 → `busy`, 603 → `rejected`, 404/484 → `invalid_number`-Fehler plus `call.ended{failed}`, sonst `failed` mit `sipCode`.
- Ist die Bridge nicht an der FRITZ!Box registriert → `error{sip_unavailable, callId}` plus `call.ended{failed}`.

### Auflegen

- Gerät: `call.hangup` → Bridge sendet BYE (verbunden) bzw. CANCEL (ausgehend, noch nicht verbunden) und bestätigt mit `call.ended{local_hangup}`.
- FRITZ!Box: BYE → `call.ended{remote_hangup}`.
- Bricht die WebSocket-Verbindung eines **verbundenen** Anrufs ab, hält die Bridge den Anruf **30 s** offen. So lange darf sich das Gerät mit `call.attach` erneut anhängen (Netzwechsel). Danach legt die Bridge auf.
  - Beim Wieder-Anhängen erzeugt die Bridge ein neues `call.offer` mit ICE-Restart; das Gerät antwortet mit `call.answer`.

## VoIP-Push (APNs)

- Topic: `<bundleId>.voip` (App: `com.jorisconrad.housephone.voip`)
- `apns-push-type: voip`, `apns-priority: 10`, `apns-expiration: 0`
- Payload:

```json
{ "type": "incoming_call", "v": 1, "callId": "…", "caller": "+4930123456", "callerName": "Oma", "bridgeId": "…" }
```

- `caller`: Nummer aus dem SIP-`From` (User-Teil). Leer, wenn unterdrückt.
- `callerName`: Anzeigename aus dem SIP-`From`, falls vorhanden.
- Antwortet APNs mit `410 Unregistered` oder `400 BadDeviceToken`, löscht die Bridge das Push-Token dieses Geräts.
- Das Gerät **muss** jeden VoIP-Push sofort per CallKit melden (Pflicht seit iOS 13). Auch dann, wenn der Anruf danach als `not_found` endet.

## Medien (WebRTC)

- Die Bridge ist immer der Offerer. Audio läuft als `sendrecv`, genau ein Audio-Track, kein Video, kein DataChannel.
- Die Bridge lauscht mit ICE-UDP-Mux auf **einem** UDP-Port (Standard `50000`). Als Kandidaten meldet sie die LAN-IP und die öffentliche IP (NAT 1:1).
- Kein Trickle-ICE in v1: SDP wird erst gesendet, wenn das ICE-Gathering abgeschlossen ist. Das Gerät wartet dafür höchstens 2 s.
- DTMF läuft nicht über RTP, sondern über `call.dtmf`. Die Bridge sendet RFC 4733 an die FRITZ!Box.
