# Umsetzung

| Teil | Dateien |
|---|---|
| Versiegelung | `bridge/internal/pushseal/`, `bridge/cmd/_pushvectors`, `docs/protocol/fixtures/crypto/push-vectors.json` |
| APNs-Client (geteilt) | `bridge/internal/apns/apns.go` (aus `internal/push/apns.go` herausgelöst) |
| Bridge-Push | `bridge/internal/push/push.go` (direkt/Relay, versiegelt), `relayclient.go` (`RelayRequest`/`RelayResponse`) |
| Relay | privates Repo `JoKeks2023/housephone-relay`: `internal/relay` (Server, Rate-Limit), `internal/apns`, `cmd/housephone-relay`, Dockerfile, Compose mit Caddy, CI |
| Konfiguration | `apns.relay` / `HOUSEPHONE_PUSH_RELAY`, `config.DefaultPushRelay` = `https://housephone.relay.jorisconrad.com`, `APNs.Mode()` |
| Status | `admin.Status.pushMode/pushRelay`, Selbsttest „Push (Relay)“ und „Push-Schlüssel“, TUI, Dashboard |
| Signaling | `pushKey` in `Hello`/`DeviceUpdate`, `store.Device.PushKey`, Prüfung auf gültigen X25519-Schlüssel |
| Add-on | 0.3.0: `push_relay`, `apns_topic` optional; Key-Felder und `addon_config`-Ordner entfernt |
| iOS | `HousephoneKit/SealedPush.swift` (Öffnen, `PushKeyStore`), `SignalingMessage`/`SignalingClient` (`pushKey`), `BridgeConnection`, `WatchBridge`, beide PushKit-Handler, `AdminView` (Push-Modus), `Localizable.xcstrings` |
| CI | Relay-Tests und gehärteter Image-Start im privaten Repo |
| Doku | ADR-0010, `signaling-v1.md` (v1.3), Bridge-README, Add-on-DOCS/CHANGELOG, `veroeffentlichung.md` |
