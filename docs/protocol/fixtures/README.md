# Protokoll-Fixtures

Kanonische Beispielnachrichten für `docs/protocol/signaling-v1.md`. Die Bridge (Go) und `HousephoneKit` (Swift) testen gegen diese Dateien:

1. Datei dekodieren.
2. Wieder enkodieren.
3. Semantisch vergleichen (JSON-Objekt-Gleichheit, Reihenfolge der Schlüssel egal).

`push.incoming_call.json` ist die APNs-VoIP-Push-Nutzlast, keine WebSocket-Nachricht.
