# Umsetzung

## Phase 1: Gerüst (2026-10-25)

| Teil | Änderung |
|---|---|
| `ios/HousephoneMac/` | `HousephoneMacApp` und `RootView`: Fenster mit Feld für den Kopplungslink, prüft ihn mit `PairingLink` aus `HousephoneKit` |
| `ios/HousephoneMac.entitlements` | App Sandbox, `network.client`, `device.audio-input` |
| `Housephone.xcodeproj` | Target `HousephoneMac` (macOS 15, `GENERATE_INFOPLIST_FILE`, Hardened Runtime, `<präfix>.housephone.mac`), synchronisierter Ordner, Scheme |
| CI | Schritt „Build Mac app“ |

Nicht verifiziert: Lauf auf einem echten Mac.

## Phase 2: Kopplung und Verbindung (2026-10-25)

| Teil | Änderung |
|---|---|
| Protokoll | Neue Plattform `macos` (`DevicePlatform.macos` im Kit, `protocol.PlatformMacOS` und `validPlatform` in der Bridge, `signaling-v1.md`). Die Admin-Rolle bleibt dem iPhone vorbehalten (`CanBeAdmin`) |
| `HousephoneMac/Bridge/MacBridge.swift` | Gekürzte `BridgeConnection`: Kopplung per Link (`BridgeHTTPClient.pair`), Schlüsselbund, `SignalingClient` mit Wegwahl (Heimnetz/öffentlich) und Netzwerkwechsel, Entkoppeln. `hello` ohne Push-Token, `mediaCapabilities: [websocket-pcma]` |
| `HousephoneMac/Features/RootView.swift` | Kopplungsfenster (Link einfügen) und Statusfenster (Bridge-Name, Verbindung, FRITZ!Box angemeldet, Heimnetz, Kennung, Kopplung aufheben) |
| Tests | `TestEndToEndMacPairs` (Bridge): Mac koppelt, erscheint mit Plattform `macos`, kein Admin |

Offen in dieser Phase: Kopplung im Heimnetz per Bonjour ohne Link (`startLanPairing` fordert bisher `ios`). Wiederverbinden nach Ruhezustand läuft nur über `scenePhase` und den Pfad-Beobachter; ein Test mit echtem Ruhezustand fehlt.

Nicht verifiziert: Lauf auf einem echten Mac, Kopplung gegen eine echte Bridge.
