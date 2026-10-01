# HTTP-Fixtures

Beispielantworten der HTTPS-Endpunkte (`GET /v1/phonebook`, `GET /v1/history`, siehe Signalisierung v1.2). Bridge und Apps testen dagegen:

- Die Bridge erzeugt aus FRITZ!Box-XML genau diese Struktur.
- Die Apps dekodieren sie.

Dazu die Kopplung (Signalisierung v2):

- `pair.request.json`, `pair.response.json`: `POST /v1/pair` mit QR-Code.
- `pair-lan.*.json`: Kopplung im Heimnetz ohne QR-Code (v2.1, ADR-0007): `start` und `offer` sind `POST /v1/pair/lan`, `reveal` geht an `POST /v1/pair/lan/{pairingId}/reveal`, `pending` und `approved` sind Antworten von `GET /v1/pair/lan/{pairingId}`. Die Werte stammen aus `crypto/lan-pairing-vectors.json`.

Diese Dateien sind keine WebSocket-Nachrichten.
