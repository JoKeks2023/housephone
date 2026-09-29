# T-0007 – Validierungsbericht

**Branch:** `feat/design-pass`, geprüfter Stand `67d06957`

## Lokal (M1, ohne Simulator und ohne `xcodebuild`)

| Prüfung | Ergebnis |
|---|---|
| `swiftc -typecheck` iPhone-App (Swift 6, `arm64-apple-ios26.0`, Upcoming Features wie im Projekt, gegen frisch gebautes `HousephoneKit`; `MediaEngine.swift` durch einen Stub ersetzt, weil das WebRTC-Paket lokal nicht aufgelöst wird) | 0 Fehler, 0 Warnungen |
| `swiftc -typecheck` Watch-App (`arm64_32-apple-watchos26.0`) | 0 Fehler, 0 Warnungen |
| Gegenprobe: absichtlich falsche Datei im selben Aufruf | Fehler werden gemeldet, die Prüfung greift |
| `swift test` in `ios/Packages/HousephoneKit` | 109 Tests, 4 Issues in `FixtureRoundTripTests` (siehe unten) |
| String-Kataloge | Keine `null`-Werte; alle neuen UI-Texte im Katalog (Skript-Abgleich Literale ↔ Schlüssel); entfernte Schlüssel ohne Verwendung im Code |

### `swift test`: Ursache der 4 Issues

`fixturesExist`, `everyMessageTypeHasAFixture` und `decodesAndReencodesIdentically(device.paired.json)` scheitern. Der Basis-Commit `d1cbeae7` hat `docs/protocol/fixtures` für Auth v2 geändert (`pair.json`/`pair.ok.json` raus, neue Fixtures rein). Der Swift-Code dazu kommt aus T-0004. Dieser Branch ändert weder `HousephoneKit` noch `docs/protocol`. Das Ergebnis ist also dasselbe wie auf `d1cbeae7` selbst.

## CI

| Lauf | Stand | Job | Ergebnis |
|---|---|---|---|
| iOS 36589054693 | `199aba03` | App build (unsigned, iOS device + embedded watch app) | success |
| iOS 36589054693 | `199aba03` | HousephoneKit tests | failure: die 4 Fixture-Issues oben |
| Bridge 36589054587 | `199aba03` | test | failure: `gofmt` meldet `cmd/_hp2vectors/main.go` aus `d1cbeae7` |
| iOS 36589606611 | `67d06957` | App build | success |
| iOS 36589606611 | `67d06957` | HousephoneKit tests | failure: dieselben 4 Fixture-Issues, keine weiteren |

Die Bridge-CI lief, obwohl dieser Branch `bridge/` nicht anfasst: Der erste Push brachte gegenüber `master` den Basis-Commit `d1cbeae7` mit. Beide roten Jobs lassen sich nur im T-0004-Bereich beheben: Swift-Fixture-Code bzw. `gofmt` des Vektor-Generators.

## Nicht verifiziert

Nichts davon lief auf einem Gerät oder Simulator. Offen für die manuelle Prüfliste aus dem Review:

- Layout auf iPhone SE, 16 und 16 Pro (Rechnung im Umsetzungsbericht), besonders SE ohne Reserve und AX-Schriftgrößen
- Glas-Morphing zwischen Steuerung und Tastenfeld, `blurReplace` der Statuszeile, Magic Replace bei Stumm
- Minimieren und Anruf-Pille (nur iOS 26.1+), Titelmenü für die Quelle in Anrufliste und Kontakten
- Lautsprecher-Schalter über `overrideOutputAudioPort` während eines echten Anrufs
- VoiceOver (Ziffern einzeln, Ansage bei Statuswechsel, keine Doppelausgabe beim Klingeln), Voice Control („Tippe Plus“, „Tippe Lautsprecher“)
- Erhöhter Kontrast hell/dunkel, „Bewegung reduzieren“
- Watch: Tastengröße auf 41 mm und 45/46 mm, Druck-Feedback, Kopplungsseite
