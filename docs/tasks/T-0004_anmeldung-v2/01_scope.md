# Scope

## Bridge
- Paket für HP2-Krypto
- Identitätsschlüssel in `/data/identity.key`
- `POST /v1/pair` v2
- Middleware für `HP2`-Auth auf allen Endpunkten und beim WebSocket-Upgrade (Nonce-Cache, ±60 s)
- versiegelte HTTPS-Antworten, verschlüsselte WebSocket-Frames
- Close-Code 4002
- `device.paired`
- CLI: `pair` wartet, `identity` zeigt den Fingerabdruck
- Probetelefon auf Software-Schlüssel umgestellt
- Compose-Härtung
- README

## Kit, iPhone und Watch
- Schlüsselverwaltung: Secure Enclave, Rückfall auf Software
- Kopplungslink v2 mit `fp`, Kopplung per HTTPS mit Prüfung der Bridge-Signatur
- Request-Signer
- WebSocket- und HTTPS-Client mit Prüfung der Bridge-Signatur und Entschlüsselung
- Zugangsdaten ohne Geheimnis, dafür mit `bridgePublicKey`
- Hinweis bei `device.paired`
- Kopplungs-UI aus dem Design-Audit (M4): Spinner im Button, „Später“ statt „Abbrechen“ nach Erfolg, Fehler-Haptik, Code-Anzeige in Vierergruppen

## Nicht drin
- Push-Payload verschlüsseln (HPHN-22)
- LAN-Kopplung ohne TLS-Terminierung
