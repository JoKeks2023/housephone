# T-0004 – Kopplung und Anmeldung v2

**Stand:** 2026-09-29

Wunsch des Nutzers: „bombensicher“, die Bridge als Docker-Container, Kopplung per Einmalcode über die Shell. Umsetzung nach `docs/architecture/ADR-0004-anmeldung-v2.md` und `docs/protocol/signaling-v2.md`.

## Kern

- Geräteschlüssel im Secure Enclave
- Bridge-Identität mit Fingerabdruck im QR-Code, von der App gepinnt
- signierte Anfragen und Antworten
- Ende-zu-Ende verschlüsselte WebSocket-Frames und HTTPS-Antworten
- 80-Bit-Code nur per `docker exec`
- `pair` wartet und zeigt an, wer den Code benutzt hat; `device.paired` an alle anderen Geräte
- Container-Härtung
