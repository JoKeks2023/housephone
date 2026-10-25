# Umsetzung

## Phase 1: Grundlage, Siri, Kurzbefehle

| Teil | Änderung |
|---|---|
| HousephoneKit | `SharedSnapshot` (Favoriten, letzte 20 Anrufe, `recentsSeenAt`, `isSetUp`), `AppGroup.identifier(forBundleIdentifier:)` (`group.<präfix>.housephone`), `SharedSnapshotStore` (JSON, atomar, schreibt nur bei Änderung), `NameMatcher` (Namenssuche ohne Groß-/Kleinschreibung und Akzente), `DeepLink` (`call`, `keypad`, `recents`), `DeepLinkKey` (Zufallsschlüssel in der App Group, Vergleich ohne frühes Ende) |
| Projekt | `HOUSEPHONE_APP_GROUP` in `Housephone.xcconfig`; Entitlements App Group und Siri; `INIntentsSupported = [INStartCallIntent]` |
| `Integration/SnapshotPublisher` | Beobachtet Favoriten, FRITZ!Box-Anrufliste, `recentsSeenAt` und Kopplung; `CallCenter.onCallRecorded` für neue Anrufe; Wechsel in den Hintergrund. Schreibt den Schnappschuss nach 300 ms Ruhe, lädt Widgets neu, aktualisiert App-Shortcut-Parameter und Schnellaktionen |
| `Integration/StartCallIntentHandler` | In-App-SiriKit über `AppDelegate.application(_:handlerFor:)`. Nummer von Siri wird übernommen. Sonst Suche in Favoriten → iPhone-Kontakten → FRITZ!Box-Telefonbuch, jede Nummer einmal; mehrere Treffer → Siri fragt nach. Wahlwiederholung → letzte gewählte Nummer. Video, Notruf, Mailbox → nicht unterstützt. `handle` → `.continueInApp`, den Anruf startet der bestehende `onContinueUserActivity` |
| `Integration/AppIntents` | `FavoriteEntity`/`FavoriteQuery` (aus dem Schnappschuss), `CallFavoriteIntent`, `CallNumberIntent`, `OpenKeypadIntent`, `ShowMissedCallsIntent`, `HousephoneShortcuts` (Phrasen deutsch, englisch in `AppShortcuts.xcstrings`) |
| `Integration/QuickActions` | Bis zu vier Favoriten am App-Icon. `SceneDelegate` (über `configurationForConnecting`) verarbeitet sie beim Kaltstart und im laufenden Betrieb |
| `AppServices.open/handle` | Zentrale Weiche für Deep Links. Anruf-Links ohne gültigen Schlüssel → Rückfrage „Anrufen?“ mit der Nummer (der Name aus dem Link wird nicht angezeigt) |
| CI | Prüft `INIntentsSupported`, `Metadata.appintents` und die App-Group-Variable in den Entitlements |
| Tests | `SharedSnapshotTests.swift` (AppGroup, Snapshot, NameMatcher), `DeepLinkTests.swift` |

Abweichung vom Plan: Die Favoriten bleiben in `UserDefaults.standard`. Erweiterungen lesen sie aus dem Schnappschuss, ein Umzug der Ablage war daher unnötig.
