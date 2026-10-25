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

## Phase 2: Widgets und Schalter

| Teil | Änderung |
|---|---|
| Projekt | Neues Target `HousephoneWidgets` (`<präfix>.housephone.widgets`, iOS 17), eingebettet über „Embed Foundation Extensions“. Neuer synchronisierter Ordner `Shared/` in App und Extension. Die App kompiliert mit `HOUSEPHONE_APP`. Eingefügt per Skript in die `project.pbxproj` (IDs deterministisch) |
| `Shared/` | `FavoriteEntity`/`FavoriteQuery` und `CallFavoriteIntent`, `OpenKeypadIntent`, `ShowMissedCallsIntent` (aus `Integration/AppIntents.swift` verschoben; `perform` nur mit `HOUSEPHONE_APP`, denn die Intents öffnen die App), `AvatarTint`/`Monogram` (aus `Design/Avatar.swift` verschoben) |
| `HousephoneWidgets/` | `FavoritesWidget` (`AppIntentConfiguration` mit `SelectFavoritesIntent`), `RecentCallsWidget`, `MissedCallsWidget` (Sperrbildschirm), `CallFavoriteControl` + `OpenKeypadControl` (iOS 18), `SnapshotEntry`/`SnapshotProvider` (liest Schnappschuss und Link-Schlüssel, neue Zeitleiste um Mitternacht), eigene Farben, Privacy-Manifest, String-Katalog (en) |
| App | `SnapshotPublisher` lädt zusätzlich die Controls neu (iOS 18) |
| CI | Extension unter `PlugIns/`, Bundle-ID, Extension-Point, App-Intents-Metadaten, App-Group-Variable in beiden Entitlements, Privacy-Manifest |

Widgets starten Anrufe über Deep Links (`Link`/`widgetURL`), nicht über `Button(intent:)`: Das funktioniert schon ab iOS 17 zuverlässig und auch auf dem Sperrbildschirm.

## Phase 3: Watch

| Teil | Änderung |
|---|---|
| HousephoneKit | `CompanionFavorites` (iPhone → Watch, Application Context, eigener Schlüssel neben `WatchPairingState`) mit Tests |
| iPhone | `WatchLink.send(favorites:)` schickt die Favoriten bei jeder Änderung und nach dem Aktivieren der Verbindung erneut, wenn sie noch nicht angekommen sind. Ausgelöst über `SnapshotPublisher.onPublish` |
| Watch-App | `WatchFavorites` (gespeichert), `PhoneLink` übernimmt den Application Context, Abschnitt „Favoriten · Vom iPhone“ auf dem Startbildschirm, `WatchSnapshotPublisher` (Favoriten + FRITZ!Box-Anrufliste bzw. eigene Anrufe in die App Group der Watch), `WatchShortcuts` (App Shortcuts mit `CallFavoriteIntent`), Deep Links: Anruf nur mit Link-Schlüssel (Komplikation) oder aus einem App Shortcut, sonst ignoriert (die Watch hat keine Rückfrage) |
| Projekt | Neues Target `HousephoneWatchWidgets` (`<präfix>.housephone.watchkitapp.widgets`, watchOS 10), eingebettet in die Watch-App. `Shared/` jetzt auch in Watch-App und Komplikationen. Die Watch-App kompiliert mit `HOUSEPHONE_WATCH_APP`. App Group in den Watch-Entitlements |
| `Shared/SystemIntents.swift` | `CallFavoriteIntent` handelt auf der Watch über `WatchServices`. Tastenfeld und verpasste Anrufe gibt es nur unter iOS |
| Komplikationen | `FavoriteComplication` (`AppIntentConfiguration`, Vorschläge je Favorit), `MissedCallsComplication` (Zeitleiste fällt mit jedem 24-Stunden-Ablauf weiter) |
| CI | Einbettung, Bundle-ID und Architektur der Komplikationen, App-Intents-Metadaten der Watch-App, App-Group-Variable in allen vier Entitlements, Privacy-Manifest |

Abweichungen vom Plan:
- **Siri auf der Watch nur für Favoriten.** watchOS bietet keinen In-App-Handler für `INStartCallIntent` (nur `handleIntent` nach einer Intents-Extension). Statt einer eigenen Intents-Extension gibt es App Shortcuts. Beliebige Namen aus dem FRITZ!Box-Telefonbuch per Siri gehen nur am iPhone.
- **Favoriten auf dem Startbildschirm** statt in `WatchContactsView`: Die Kontakte erscheinen nur mit FRITZ!Box-Telefonbuch, die Favoriten sollen auch ohne es sichtbar sein.
