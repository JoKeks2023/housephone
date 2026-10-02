# Umfang

- Relay `housephone-relay` (Go, Docker Compose mit Cloudflare Tunnel) im privaten Repository `JoKeks2023/housephone-relay` (Vorgabe 2026-10-02: nicht im Haupt-Repo).
- Ende-zu-Ende-Versiegelung der Push-Nutzlast (`internal/pushseal`), Testvektoren für Go und Swift.
- Bridge: `apns.relay`, Relay als Standard ohne eigenen Key, eigener Key bleibt möglich; Status, Selbsttest, TUI, Dashboard.
- Signaling v1.3: `pushKey` in `hello` und `device.update`.
- App und Watch: Push-Schlüssel im Schlüsselbund, versiegelte Pushes öffnen.
- Home-Assistant-Add-on 0.3.0: APNs-Felder raus, optional `push_relay`.

Nicht im Umfang: Relay deployen (kein Zugang zum VPS), Datenschutzangaben im App Store. Domain: zuerst `housephone.relay.jorisconrad.com` (Vorgabe 2026-10-02), dann `https://push.jorisconrad.com/housephone`, weil das kostenlose Cloudflare-Zertifikat nur eine Ebene abdeckt.
