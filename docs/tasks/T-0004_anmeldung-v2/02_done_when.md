# Fertig, wenn

- [ ] **Testvektoren:** Bridge und Kit testen erfolgreich gegen `fixtures/crypto/hp2-vectors.json`.
- [ ] **Angriffstests der Bridge:**
  - wiederholter Nonce → 401
  - Zeitstempel ±61 s → 401 `clock_skew`
  - fremder Schlüssel → 401
  - veränderter Body → 401
  - Code zweimal → 403
  - falscher `proof` → 403
  - verändertes, vertauschtes oder wiederholtes WebSocket-Frame bzw. Textframe → 4002
- [ ] **Angriffstests der App (Kit):**
  - falsche Bridge-Signatur, fehlender Header oder anderer Bridge-Schlüssel als gepinnt → Abbruch
  - `fp` passt nicht zum Schlüssel → Kopplung abgelehnt
- [ ] **E2E im Prozess:** Kopplung v2 → WebSocket → eingehender und ausgehender Anruf (WebRTC und `websocket-pcma`) → HTTPS für Telefonbuch und Anrufliste, alles versiegelt.
- [ ] **Probetelefon:** Echter Anruf über die FRITZ!Box funktioniert weiter (Vortest wie HPHN-8).
- [ ] **CI:** Bridge und iOS grün.
