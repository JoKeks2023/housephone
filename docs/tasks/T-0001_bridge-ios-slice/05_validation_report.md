# Validierungsbericht T-0001

**Stand:** 2026-09-29

## Belegt

| Prüfung | Ergebnis | Beleg |
|---|---|---|
| Bridge: `go vet`, `go test -race ./...` (12 Pakete) | grün | CI https://github.com/JoKeks2023/housephone/actions/runs/36544892032 |
| Bridge: E2E (Fake-FRITZ!Box + Test-Gerät mit pion-WebRTC) | eingehend G.722 bidirektional, ausgehend mit Ringing/Connected, DTMF, BYE – 3× wiederholt grün | `bridge/internal/app` |
| Bridge: Docker-Image | Build plus `version`-Smoke-Test in der CI grün | Job `docker` |
| Kit: `swift test` | 52 Tests in 6 Suites grün, inkl. Round-Trip aller Fixtures | lokal und in der CI |
| iOS-App: unsignierter Build `generic/platform=iOS` | `BUILD SUCCEEDED`, keine Warnungen | CI https://github.com/JoKeks2023/housephone/actions/runs/36545230449 |
| Protokoll Go ↔ Swift | beide Seiten testen gegen dieselben Dateien in `docs/protocol/fixtures/` | – |

## Nicht belegt (braucht Hardware, siehe HPHN-8)

- Registrierung an der echten FRITZ!Box 6591 und Codec-Aushandlung mit ihr (G.722, RFC 4733)
- APNs-Zustellung und CallKit-Oberfläche auf einem echten iPhone
- libwebrtc (iOS) ↔ pion: Answer-Codec-Reihenfolge, Ton, ICE über Mobilfunk mit UDP-Freigabe
- Cloudflare Tunnel, QR-Kopplung, Kontakte, `INStartCallIntent`

**Status: Code fertig und automatisiert geprüft, funktional nicht verifiziert.**
