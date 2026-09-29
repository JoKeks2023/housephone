# ADR-0004: Kopplung und Anmeldung v2 – Ende-zu-Ende gesichert

- Status: angenommen (2026-09-29)
- Ticket: HPHN-27
- Ersetzt die Bearer-Anmeldung aus Signalisierung v1. Es gibt keinen Parallelbetrieb, weil noch keine echten Geräte gekoppelt sind.

## Kontext und Bedrohungsmodell

Die Bridge ist über Cloudflare Tunnel (TLS endet bei Cloudflare) und UDP 50000 aus dem Internet erreichbar. Anforderung des Nutzers: „bombensicher“. Geschützt werden soll gegen:

1. **Mitleser an der TLS-Terminierung.** Das sind Cloudflare, Firmen-/Hotel-WLAN mit eigenem Root-Zertifikat oder ein kompromittierter Tunnel. Er darf keine Anmeldedaten erbeuten.
2. **Aktiver Angreifer an derselben Stelle.** Er darf keine Nachrichten einschleusen oder verändern. Das ist kritisch: Mit einem vertauschten DTLS-Fingerabdruck im SDP könnte er WebRTC-Gespräche mithören. Ebenso darf er keine falsche Bridge vortäuschen.
3. **Gestohlene Geräte-Backups oder ein kopierter Speicher.** Anmeldeschlüssel dürfen nicht kopierbar sein.
4. **Gelesene `devices.json` der Bridge.** Sie darf keine Anmeldung ermöglichen.
5. **Wiederholte oder abgefangene Anfragen.**

Nicht im Modell: Wer Root auf dem Server hat, gilt als vertrauenswürdig. Kopplungscodes entstehen deshalb nur per `docker exec`.

## Entscheidung

### Schlüssel

| Wer | Schlüssel | Wo |
|---|---|---|
| Gerät (iPhone, Watch) | P-256 ECDSA, **Secure Enclave**, nicht exportierbar | Access Control nur `.privateKeyUsage` mit `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`. Signieren muss bei gesperrtem Gerät für eingehende Anrufe möglich sein, deshalb **keine** Biometrie. Rückfall ohne Secure Enclave (`SecureEnclave.isAvailable == false`): Software-Schlüssel im Schlüsselbund, `ThisDeviceOnly`. |
| Bridge | Ed25519 | `/data/identity.key` (0600), beim ersten Start erzeugt |
| Probetelefon | P-256 als Software-Schlüssel | Datei neben den Zugangsdaten (0600) |

- **Fingerabdruck der Bridge:** `base64url(SHA-256(ed25519PublicKeyRaw))`, ohne Padding.
- **Angezeigt** wird er in CLI und TUI als Hex, in Vierergruppen.

### Kopplung

1. `docker compose exec housephone-bridge housephone-bridge pair [-name …]` erzeugt den Code und zeigt QR-Code und Link. Danach **wartet** der Befehl und meldet, welches Gerät (Name, Modell, Plattform) den Code benutzt hat. Strg-C widerruft den Code.
2. Der Link hat die Form `housephone://pair?v=2&url=<wss-URL>&code=<code>&fp=<Fingerabdruck>&name=<Bridge-Name>`.
   - Der **Code** hat 16 Zeichen aus `A-Z2-9` ohne `0 O 1 I`, also **80 Bit**.
   - Er ist 10 Minuten gültig und einmalig.
   - Angezeigt wird er als `XXXX-XXXX-XXXX-XXXX`.
3. Die Kopplung läuft für iPhone **und** Watch über `POST /v1/pair` (HTTPS). Die WebSocket-Nachricht `pair` entfällt. Der Body lautet:
   `{code, deviceName, platform, model?, publicKey, nonce, proof}`
   - `publicKey`: X9.63-unkomprimierter P-256-Schlüssel (65 Byte), base64url
   - `nonce`: 16 Zufallsbytes, base64url
   - `proof`: ECDSA-Signatur (roh `r‖s`, 64 Byte, base64url) des Geräts über
     `"HP2-PAIR-PROOF\n" + code + "\n" + nonce + "\n" + publicKey`
4. Die Bridge prüft den Code (atomar, einmalig), den `proof` und das Rate-Limit. Sie speichert den öffentlichen Schlüssel und antwortet:
   `200 {deviceId, bridgeId, bridgeName, bridgePublicKey, signature}`
   - `signature` ist die Ed25519-Signatur über
     `"HP2-PAIR\n" + bridgeId + "\n" + deviceId + "\n" + publicKey + "\n" + nonce + "\n" + code`
5. Das Gerät prüft `base64url(SHA-256(bridgePublicKey)) == fp` aus dem Link und die Signatur. Erst dann speichert es `deviceId`, `bridgeId`, `bridgePublicKey` und `url`, also die gepinnte Bridge-Identität. Passt etwas nicht, bricht die Kopplung mit „Diese Bridge ist nicht die aus dem QR-Code“ ab.
6. **Sichtbarkeit:**
   - Alle verbundenen Geräte bekommen `device.paired {deviceName, platform, pairedAt}`, die App zeigt einen Hinweis.
   - Die Bridge loggt die Kopplung mit Gerätename und Fingerabdruck des Geräteschlüssels.
7. **Companion-Kopplung (Watch):** wie gehabt über das iPhone (`pair.companion.request`). Die Uhr erzeugt ihren **eigenen** Secure-Enclave-Schlüssel und koppelt sich wie oben per `POST /v1/pair`.

### Anmeldung jeder Anfrage (HTTPS und WebSocket-Upgrade)

**Anfrage-Header:**

```
Authorization: HP2 id=<deviceId>, ts=<Unix-Sekunden>, nonce=<b64url 16 B>, epk=<b64url X25519 32 B>, sig=<b64url 64 B>
```

- `epk` ist ein **pro Anfrage neuer** X25519-Schlüssel des Geräts, als Software-Schlüssel.
- `sig` ist die ECDSA-Signatur (P-256, SHA-256, roh `r‖s`) mit dem Geräteschlüssel über (Zeilen mit `\n` verbunden):

```
HP2-AUTH
<METHODE in Großbuchstaben>
<Pfad inkl. Query, wie gesendet>
<bridgeId>
<deviceId>
<ts>
<nonce>
<epk>
<hex(SHA-256(Request-Body))>          (leerer Body: SHA-256 von "")
```

**Die Bridge prüft:**
- das Gerät existiert,
- `|now − ts| ≤ 60 s`,
- `nonce` war für dieses Gerät in den letzten 120 s noch nicht da (begrenzter Cache),
- die Signatur stimmt.

Sonst antwortet sie `401` ohne Details. Signaturen werden in konstanter Zeit bzw. mit den Standardbibliotheken geprüft.

**Antwort-Header**, bei jedem Status einschließlich `101` und Fehlern:

```
HP2-Bridge: epk=<b64url X25519 32 B>, sig=<b64url Ed25519 64 B>
```

- `sig` ist die Ed25519-Signatur mit dem Bridge-Schlüssel über:

```
HP2-BRIDGE
<bridgeId>
<deviceId>
<nonce aus der Anfrage>
<epk des Geräts>
<epk der Bridge>
<HTTP-Status>
<hex(SHA-256(Response-Body wie gesendet))>   (bei 101: SHA-256 von "")
```

- Das Gerät prüft diese Signatur mit dem **gepinnten** Bridge-Schlüssel, **bevor** es die Antwort verwendet. Fehlt der Header oder ist er falsch, verwirft es die Antwort und zeigt „Bridge nicht vertrauenswürdig“; es gibt keinen Rückfall.

### Sitzungsschlüssel und Verschlüsselung

- **Gemeinsames Geheimnis:** `shared = X25519(epk-Gerät, epk-Bridge)`.
- **Schlüsselableitung:** `okm = HKDF-SHA256(ikm = shared, salt = nonce (16 B), info = "HP2-KEYS\n" + bridgeId + "\n" + deviceId + "\n" + epkGerät + "\n" + epkBridge, L = 64)`
  - `kGeräteSeite = okm[0:32]` für Gerät → Bridge
  - `kBridgeSeite = okm[32:64]` für Bridge → Gerät
- **AEAD:** ChaCha20-Poly1305, AAD `"HP2"`. Die Nonce hat 12 Byte: 4 Nullbytes plus Zähler (uint64, Big Endian). Jede Richtung führt ihren eigenen Zähler, beginnend bei 0.

**WebSocket (`/v1/ws`):**
- Nach dem Upgrade ist **jede** Nachricht in beide Richtungen ein **binärer** WebSocket-Frame mit `seal(Richtungsschlüssel, Zähler, Klartext)`.
- Der Klartext besteht aus Typ-Byte und Inhalt:
  - `0x00`: JSON-Nachricht wie in v1 (`{type, payload}`)
  - `0x01`: 160 Byte A-law (Watch-Medienweg)
- Textframes, falsche Tags oder Zählerlücken → sofortiges Schließen mit Close-Code `4002` („integrity“).
- `hello` bleibt die erste Nachricht, jetzt verschlüsselt.
- **Folge:** Signalisierung einschließlich SDP und DTLS-Fingerabdrücken sowie der Watch-Ton sind Ende-zu-Ende vertraulich und unveränderbar, auch gegenüber Cloudflare.

**HTTPS-Antworten** (`/v1/device`, `/v1/calls/{id}`, `/v1/phonebook`, `/v1/history`, `/v1/health` mit Auth):
- Der Body ist `seal(kBridgeSeite, 0, JSON)` mit `Content-Type: application/vnd.housephone.sealed`. Die Signatur deckt den verschlüsselten Body ab.
- Telefonbuch und Anrufliste sind damit Ende-zu-Ende verschlüsselt.
- `ETag`/`304` bleiben erhalten; ein `304` hat keinen Body.

**HTTPS-Anfragen:**
- Anfrage-Bodies (`PUT /v1/device`) sind **nicht** verschlüsselt, weil der Bridge-`epk` erst mit der Antwort kommt. Sie sind durch die Signatur unveränderbar und über TLS vertraulich. Sie enthalten nur Push-Token und Gerätenamen.

`/v1/health` ohne Auth bleibt unverschlüsselt und liefert nur `{status}` ohne Version und Registrierungsstatus.

### Widerruf und Container

- **Widerruf:** `devices remove` wirkt sofort (HPHN-20): offene Sitzungen werden geschlossen und laufende Anrufe beendet.
- **Docker Compose:** `read_only: true`, `tmpfs: /tmp`, `security_opt: [no-new-privileges:true]`, `cap_drop: [ALL]`, Benutzer ohne Root. Schreibbar ist nur `/data`.

## Restrisiken (bewusst)

- **Kopplungsanfrage:** Der Code reist nur TLS-geschützt. Ein aktiver Angreifer an der TLS-Terminierung könnte die Anfrage abfangen und sich selbst koppeln. Das echte Gerät bekäme dann einen Fehler, und die Fremdkopplung wäre sichtbar: in `pair`, in der TUI und per `device.paired` auf allen Geräten. Gegenmittel: Kopplung im Heim-WLAN direkt gegen die LAN-Adresse – später als Option.
- **APNs-Nutzlast:** Nummer und Name des Anrufers gehen über Apple. Die Ende-zu-Ende-Verschlüsselung der Push-Nutzlast kommt mit dem Push-Relay (HPHN-22).
- **Zeitfenster ±60 s:** Geräte mit falscher Uhr können sich nicht anmelden. Die Bridge meldet in der `401`-Antwort nur `clock_skew`, falls die Signatur sonst gültig wäre.

## Testvektoren

`docs/protocol/fixtures/crypto/hp2-vectors.json` enthält feste Schlüssel und alle erwarteten Zwischenwerte (Signatur-Eingaben, gemeinsames Geheimnis, HKDF, verschlüsselte Frames und Antworten).
- Die X25519- und Ed25519-Schlüssel stammen aus RFC 7748 bzw. RFC 8032.
- Erzeugt mit `go run ./cmd/_hp2vectors` im Ordner `bridge/`.
- Gegengeprüft am 2026-09-29 mit Apple CryptoKit: HKDF, ChaChaPoly, ECDSA-Verifikation, Ed25519 und Fingerabdruck stimmen überein.
- Bridge (Go) und Kit (Swift) müssen gegen diese Datei testen.

## Verworfene Alternativen

| Alternative | Warum nicht |
|---|---|
| Bearer-Geheimnis (v1) | Geht über die Leitung, ist kopierbar und schützt nicht vor Manipulation an der TLS-Terminierung. |
| mTLS mit Client-Zertifikat | Endet ebenfalls bei Cloudflare, wenn nicht durchgereicht. Zertifikats-Handling in iOS ist aufwendig. Schützt die Signalisierung nicht Ende zu Ende. |
| Noise-Protokoll-Bibliothek | Fremdabhängigkeit auf iOS/watchOS. Die Kombination oben nutzt nur CryptoKit bzw. die Go-Standardbibliothek plus `x/crypto`. |
| Biometrie für jede Signatur | Eingehende Anrufe bei gesperrtem Gerät wären unmöglich. |
