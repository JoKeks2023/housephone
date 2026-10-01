# Housephone Signalisierung v2

**v2 = v1 (inkl. Erweiterungen v1.1/v1.2) mit neuer Kopplung, Anmeldung und Ende-zu-Ende-Absicherung.** Die kryptografischen Details stehen in `docs/architecture/ADR-0004-anmeldung-v2.md`, die Testvektoren in `fixtures/crypto/hp2-vectors.json`. Alles, was hier nicht geändert wird, gilt aus `signaling-v1.md` unverändert, insbesondere alle Anruf-Nachrichten und -Abläufe.

## Entfällt

- `Authorization: Bearer …` und das `deviceSecret`.
- WebSocket-Nachrichten `pair` und `pair.ok`. Gekoppelt wird nur noch per HTTPS.
- Textframes auf `/v1/ws`.
- Fixtures `pair.json`, `pair.ok.json`. Ersatz: `http/pair.request.json`, `http/pair.response.json`.

## Neu bzw. geändert

| Was | Neu |
|---|---|
| Kopplungslink | `housephone://pair?v=2&url=…&lan=…&code=<16 Zeichen>&fp=<Fingerabdruck>&name=…` (`lan` siehe „Zugänge“) |
| `POST /v1/pair` (iPhone und Watch) | Anfrage `{code, deviceName, platform, model?, publicKey, nonce, proof}`, Antwort `{deviceId, bridgeId, bridgeName, bridgePublicKey, signature}`. Fehler als `error`-Payload mit 400/403/429. Die Antwort ist **nicht** verschlüsselt, aber signiert; das Gerät prüft sie gegen `fp`. |
| Jede andere HTTPS-Anfrage und der WS-Upgrade | Header `Authorization: HP2 …`; die Antwort trägt `HP2-Bridge: …`. |
| HTTPS-Antwort-Bodies (angemeldet) | `application/vnd.housephone.sealed` = `seal(kBridgeSeite, 0, JSON)`. Status und `ETag` bleiben im Klartext; 304 ohne Body. |
| `/v1/ws` nach dem Upgrade | Nur binäre Frames, jeder `seal(Richtungsschlüssel, Zähler, Typ-Byte ‖ Inhalt)`. Typ `0x00` = JSON-Nachricht wie in v1, `0x01` = 160 Byte A-law. |
| `GET /v1/health` ohne Auth | nur `{"status":"ok"}` |
| Neue Nachricht Bridge → Gerät | `device.paired {deviceName, platform, pairedAt}` an alle verbundenen Geräte, wenn ein neues Gerät gekoppelt wurde |
| `401`-Antworten | Body `{"code":"unauthorized"}` bzw. `{"code":"clock_skew"}`, wenn Zeitstempel falsch, Signatur sonst gültig |
| `welcome`, `pair.companion` | zusätzlich `lanUrl` (optional): URL des privaten Zugangs, siehe „Zugänge“ |

## Zugänge

Die Bridge lauscht auf zwei Adressen. Beide sprechen dasselbe Protokoll mit derselben HP2-Anmeldung und demselben Pinning; sie unterscheiden sich nur darin, was sie anbieten und wen sie hereinlassen.

| | Öffentlich (`bridge.listen`, z. B. `127.0.0.1:8080`) | Privat (`bridge.privateListen`, Standard `:8081`) |
|---|---|---|
| Erreichbar über | Cloudflare Tunnel (`url` im Link, `wss://`) | Heimnetz bzw. Tailscale (`lan` im Link, `lanUrl` in `welcome`, meist `ws://`) |
| Wer darf | jeder, der den Tunnel erreicht | nur Absender aus `bridge.trustedNetworks`, sonst `403` `{"code":"home_network_required"}` |
| `/v1/ws`, `GET /v1/calls/{id}`, `PUT`/`DELETE /v1/device`, `GET /v1/phonebook`, `GET /v1/history`, `/v1/health` | ja (HP2) | ja (HP2) |
| `POST /v1/pair` | nein, `404` | ja |
| `POST /v1/pair/lan`, `…/reveal`, `GET /v1/pair/lan/{id}` (v2.1) | nein, `403` `home_network_required` | ja |
| `pair.companion.request` | `error` `home_network_required` | ja |

- **Absenderprüfung:** Nur die TCP-Gegenstelle zählt (`RemoteAddr`), nie `CF-Connecting-IP` oder `X-Forwarded-For`. Standard sind `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`, `169.254.0.0/16` und `fe80::/10`. Mit `bridge.tailscale: true` kommen `100.64.0.0/10` und `fd7a:115c:a1e0::/48` dazu.
- **Loopback zählt nicht:** cloudflared verbindet sich von `127.0.0.1`. Ließe der private Zugang Loopback zu, käme jeder über den Tunnel an die Kopplung, sobald jemand den Tunnel auf `:8081` zeigen lässt. Wer Loopback wirklich braucht (Tests), trägt es ausdrücklich in `trustedNetworks` ein.
- **`lanUrl`:** `bridge.lanUrl`, sonst `ws://<LAN-IP der Bridge>:<Port von privateListen>/v1/ws`. Die LAN-IP ist `sip.bindHost` bzw. die Adresse, über die die Bridge die FRITZ!Box erreicht.
- **Kopplungslink:** `lan` steht direkt nach `url`. Die App koppelt nur über `lan`; antwortet der private Zugang nicht (oder mit `home_network_required`), zeigt sie „Zum Koppeln ins Heim-WLAN oder Tailscale“. Links älterer Bridges ohne `lan` koppeln weiter über `url`.
- **Verbindungswahl der App:** Vor jedem Verbindungsaufbau und bei jedem Netzwechsel (`NWPathMonitor`) probiert die App `lanUrl` mit rund 1 s TCP-Verbindungsaufbau, sofern WLAN, Ethernet oder ein VPN (Tailscale) aktiv ist. Klappt das, verbindet sie sich privat, sonst über `url`. Wechselt die bessere Route, baut sie neu auf; laufende Anrufe hängen sich wie nach jedem Reconnect per `call.attach` wieder an.
- **Watch:** Das iPhone fordert Companion-Codes nur an, solange es privat verbunden ist; die automatische Kopplung der Watch läuft also nur im Heimnetz. Die Watch koppelt über `pair.companion.lanUrl` und telefoniert danach über `url`.
- **Keine Portfreigabe:** Der private Zugang gehört nie ins Internet. Die Absenderprüfung ist eine zweite Linie, keine Firewall.

## Close-Codes auf `/v1/ws`

| Code | Bedeutung |
|---|---|
| 1000 | normal (z. B. nach `device.unpair`) |
| 4001 | ersetzt durch neue Verbindung desselben Geräts |
| 4002 | Integritätsfehler: Tag falsch, Zähler falsch, Textframe |
| 4003 | Gerät widerrufen (`devices remove`) |

## Kopplungscode

- 16 Zeichen aus `A-Z2-9` ohne `0 O 1 I`, also 80 Bit.
- 10 min gültig, einmalig.
- Anzeige als `XXXX-XXXX-XXXX-XXXX`; Eingabe mit oder ohne Bindestriche, Groß- und Kleinschreibung egal.

## Koppeln im Heimnetz ohne QR-Code (v2.1)

Begründung und Sicherheitsargument: `docs/architecture/ADR-0007-koppeln-im-heimnetz.md`. Testvektoren: `fixtures/crypto/lan-pairing-vectors.json`, Beispiele: `fixtures/http/pair-lan.*.json`. Nur auf dem privaten Zugang.

**Finden:** Bonjour-Dienst `_housephone._tcp` auf dem Port von `privateListen`. Instanzname = Bridge-Name. TXT: `txtvers=1`, `proto=hp2`, `pair=lan`, `fp=<erste 8 Zeichen des Fingerabdrucks>`. Die App verbindet sich mit `ws://<IPv4>:<Port>/v1/ws` als `lanUrl`.

**Ablauf:**

| Schritt | Anfrage | Antwort |
|---|---|---|
| 1 | `POST /v1/pair/lan` `{deviceName, platform: "ios", model?, publicKey, commitment}` | `200` `{pairingId, bridgeId, bridgeName, bridgePublicKey, bridgeEphemeral, bridgeNonce, expiresAt, signature}` |
| 2 | `POST /v1/pair/lan/{pairingId}/reveal` `{deviceEphemeral, deviceNonce, proof}` | `200` `{status: "pending", expiresAt}` |
| 3 | `GET /v1/pair/lan/{pairingId}?wait=<s, max 25>` (Long-Poll) | `200` `{status: "pending" \| "denied" \| "expired"}` oder `{status: "approved", sealed}` |

Kanonische Eingaben (Zeilen mit `\n`, Werte Base64url ohne Padding):

- `commitment = b64(SHA-256("HP2-LAN-COMMIT", publicKey, deviceEphemeral, deviceNonce))`
- Signatur der Bridge in Schritt 1: Ed25519 über `"HP2-LAN-OFFER", pairingId, bridgeId, publicKey, commitment, bridgePublicKey, bridgeEphemeral, bridgeNonce`
- `T = SHA-256("HP2-LAN-TRANSCRIPT", pairingId, bridgeId, publicKey, commitment, bridgePublicKey, bridgeEphemeral, bridgeNonce, deviceEphemeral, deviceNonce)`
- `proof` = ECDSA P-256 (Geräteschlüssel) über `"HP2-LAN-PROOF", b64(T)`
- `SAS = uint32_be(HKDF-SHA256(X25519, salt = T, info = "HP2-LAN-SAS", 4)) mod 1000000`, sechsstellig mit führenden Nullen; Anzeige „123 456“
- Freigabe-Schlüssel `K = HKDF-SHA256(X25519, salt = T, info = "HP2-LAN-SEAL", 32)`
- `sealed = b64(ChaCha20-Poly1305(K, Zähler 0, AAD "HP2", JSON {deviceId, bridgeId, bridgeName, publicUrl, lanUrl, signature}))`, `signature` = Ed25519 über `"HP2-LAN-APPROVED", b64(T), deviceId, bridgeId, publicUrl, lanUrl`

**Regeln:**

- Nur `platform: "ios"`; die Watch koppelt weiter über `pair.companion`.
- Schritte 2 und 3 nur von derselben Absenderadresse (bzw. /64) wie Schritt 1, sonst `404` bzw. `expired`.
- Falsches Commitment, falscher Beweis oder ungültiger Schlüssel in Schritt 2: `403` `pairing_invalid`, die Anfrage ist danach weg.
- Eine Anfrage wartet 2 Minuten auf den Admin; Ergebnis danach noch 1 Minute abholbar. Unbekannt oder abgelaufen: `{status: "expired"}`.
- Grenzen: 5 Starts pro Absender in 10 Minuten, 30 pro Stunde insgesamt, höchstens 5 offen (`429` `pairing_rate_limited`). Ein neuer Start derselben Adresse ersetzt ihre alte Anfrage.
- Freigabe nur durch einen Admin: TUI (Tab „Kopplung“), `housephone-bridge devices pending|approve|deny` oder das HA-Dashboard, jeweils mit Anzeige des Codes.
- Nach `approved` prüft das Gerät die Signatur mit dem Bridge-Schlüssel aus Schritt 1 und pinnt ihn. `publicUrl` leer: Das Gerät nutzt `lanUrl` für beides.
