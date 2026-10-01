# Umsetzung

| Bereich | Dateien |
|---|---|
| Krypto (Go) | `bridge/internal/hp2/lanpair.go`, `lanclient.go`, `lanpair_test.go` |
| Protokoll-Typen | `bridge/internal/protocol/lanpair.go` |
| Endpunkte, Zustand, Grenzen | `bridge/internal/signaling/lanpair.go`, `server.go` (Routen), `lanpair_test.go` |
| Bonjour | `bridge/internal/bonjour/` (`hashicorp/mdns` v1.0.7), Start in `internal/app/app.go` (`announce`) |
| Konfiguration | `bridge.bonjour` (`config.go`, `config.example.yaml`), Add-on-Option `bonjour` (`haoptions.go`, `ha-addon/…/config.yaml`, Übersetzungen) |
| Admin | `internal/admin` (`/v1/lan-pairings`, approve/deny, `GroupSAS`), `internal/app/admin.go` |
| Freigabe | TUI `internal/tui/tui.go`, CLI `cmd/housephone-bridge/lanpair.go`, Dashboard `internal/dashboard` (Karte, `POST lan/{id}/approve|deny`) |
| Vektoren, Fixtures | `bridge/cmd/_lanvectors`, `docs/protocol/fixtures/crypto/lan-pairing-vectors.json`, `docs/protocol/fixtures/http/pair-lan.*.json` |
| Swift-Kit | `HousephoneKit/LanPairing.swift`, `BridgeHTTPClient.swift` (`startLanPairing`, `waitForLanApproval`, `cancelLanPairing`), `LanPairingTests.swift` |
| iOS-App | `Bridge/BridgeBrowser.swift` (`NWBrowser`, Auflösen auf IPv4), `Bridge/BridgeConnection.swift` (`adopt`, LAN-Methoden), `Features/Onboarding/LanPairingView.swift`, `OnboardingView.swift`, `Info.plist` (`NSBonjourServices`), `Localizable.xcstrings` (+25 Texte de/en) |
| Doku | ADR-0007, `signaling-v2.md`, `README.md`, `ha-addon/…/DOCS.md`, `docs/roadmap.md` |

**Entscheidungen beim Bauen:**

- SAS-Vergleich durch den Admin (Code wird auf der Bridge angezeigt), wie im Ticket. Die CLI fragt nach; ohne Rückfrage nur mit `-code`, der passen muss. Die TUI verlangt nach `a` ein `j`.
- Freigabe-Ergebnis zusätzlich versiegelt (Schlüssel aus dem X25519-Geheimnis): Im LAN läuft `ws://`/`http://` unverschlüsselt, und die Antwort enthält die öffentliche URL.
- Anfragen sind an die Absenderadresse gebunden; ein neuer Start derselben Adresse ersetzt den alten (Wiederholen in der App).
- Nur `ios`; die Watch bleibt bei `pair.companion`.
- Abweichung: Das Ticket nennt „Admin-iPhone per Face ID“ als Freigabeweg; das ist HPHN-37 und nicht gebaut. Die Admin-API ist die Naht dafür.
