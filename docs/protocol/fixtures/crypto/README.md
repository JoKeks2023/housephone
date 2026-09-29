# Krypto-Testvektoren (HP2)

`hp2-vectors.json`: feste Schlüssel und erwartete Werte für ADR-0004.
- Deterministisch: HKDF, ChaCha20-Poly1305, Ed25519.
- ECDSA-Signaturen sind zufällig und nur zum **Verifizieren** gedacht.
- Neu erzeugen: `cd bridge && go run ./cmd/_hp2vectors > ../docs/protocol/fixtures/crypto/hp2-vectors.json`. Danach müssen beide Implementierungen erneut grün laufen.
