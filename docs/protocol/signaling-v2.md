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
| Kopplungslink | `housephone://pair?v=2&url=…&code=<16 Zeichen>&fp=<Fingerabdruck>&name=…` |
| `POST /v1/pair` (iPhone und Watch) | Anfrage `{code, deviceName, platform, model?, publicKey, nonce, proof}`, Antwort `{deviceId, bridgeId, bridgeName, bridgePublicKey, signature}`. Fehler als `error`-Payload mit 400/403/429. Die Antwort ist **nicht** verschlüsselt, aber signiert; das Gerät prüft sie gegen `fp`. |
| Jede andere HTTPS-Anfrage und der WS-Upgrade | Header `Authorization: HP2 …`; die Antwort trägt `HP2-Bridge: …`. |
| HTTPS-Antwort-Bodies (angemeldet) | `application/vnd.housephone.sealed` = `seal(kBridgeSeite, 0, JSON)`. Status und `ETag` bleiben im Klartext; 304 ohne Body. |
| `/v1/ws` nach dem Upgrade | Nur binäre Frames, jeder `seal(Richtungsschlüssel, Zähler, Typ-Byte ‖ Inhalt)`. Typ `0x00` = JSON-Nachricht wie in v1, `0x01` = 160 Byte A-law. |
| `GET /v1/health` ohne Auth | nur `{"status":"ok"}` |
| Neue Nachricht Bridge → Gerät | `device.paired {deviceName, platform, pairedAt}` an alle verbundenen Geräte, wenn ein neues Gerät gekoppelt wurde |
| `401`-Antworten | Body `{"code":"unauthorized"}` bzw. `{"code":"clock_skew"}`, wenn Zeitstempel falsch, Signatur sonst gültig |

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
