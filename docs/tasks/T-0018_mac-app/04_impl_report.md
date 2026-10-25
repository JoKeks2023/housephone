# Umsetzung

## Phase 1: Gerüst (2026-10-25)

| Teil | Änderung |
|---|---|
| `ios/HousephoneMac/` | `HousephoneMacApp` und `RootView`: Fenster mit Feld für den Kopplungslink, prüft ihn mit `PairingLink` aus `HousephoneKit` |
| `ios/HousephoneMac.entitlements` | App Sandbox, `network.client`, `device.audio-input` |
| `Housephone.xcodeproj` | Target `HousephoneMac` (macOS 15, `GENERATE_INFOPLIST_FILE`, Hardened Runtime, `<präfix>.housephone.mac`), synchronisierter Ordner, Scheme |
| CI | Schritt „Build Mac app“ |

Nicht verifiziert: Lauf auf einem echten Mac.
