# T-0007 – Umsetzungsbericht

**Branch:** `feat/design-pass` (von `d1cbeae7`)
**Grundlage:** `docs/reviews/2026-09-29_design-hig-appstore.md`
**Nicht in diesem Branch:** Onboarding (`Features/Onboarding/*`), Kopplung, Signalisierung, Credentials. Das liegt im T-0004-Fork.

## Befunde

| Befund | Status | Umsetzung |
|---|---|---|
| H1 Tastenfeld im Anruf läuft über | umgesetzt | `InCallView`: im Tastenfeld-Modus kompakter Kopf ohne Avatar; die Tastengröße folgt der verfügbaren Höhe (52–74 pt). Rechnung unten. |
| H2 „Alles löschen“ ohne Rückfrage | umgesetzt | `RecentsView`: Menü „Mehr“ → `confirmationDialog` „Alle Anrufe löschen?“ mit destruktivem „Alle löschen“ und Erklärtext. |
| H3 Kontrast Light Mode unter AA | umgesetzt | Asset-Farben `Call`, `Danger`, `Warning` plus Textvarianten `DangerText`, `WarningText`, `AccentText`, jeweils hell/dunkel/erhöhter Kontrast. Tabelle unten. |
| H4 Wählpfad mit VoiceOver/Voice Control | umgesetzt | Nummern werden Ziffer für Ziffer gesprochen (`accessibilitySpeechSpellsOutCharacters`); „Stern“/„Raute“ als Namen; `accessibilityAction` „Plus“ auf der 0 und „Nummer löschen“ auf Löschen; `accessibilityInputLabels` am Audio-Knopf. |
| H5 Dynamic Type eingefroren | umgesetzt | Nummernanzeige über `@ScaledMetric(relativeTo: .largeTitle)`; Name im Anruf als `.title`/`.title3`; Zeilen mit `AnyLayout` für Bedienungshilfen-Größen. Tastenziffern und Symbole in runden Knöpfen skalieren mit dem Knopf (wie in der Telefon-App). |
| M1 Umschalter uneinheitlich | umgesetzt | Die Quelle (iPhone/FRITZ!Box) sitzt im Titelmenü (`toolbarTitleMenu` + `navigationSubtitle`) und wird pro Liste gespeichert. Segmentierter Umschalter nur noch für den Filter. |
| M2 Akzent dekorativ verbraucht | umgesetzt | Avatare neutral (`.fill.tertiary`, `.secondary`); Zeilensymbole `.tertiary`. |
| M3 Onboarding-Glyph | zurückgestellt | Liegt in `Features/Onboarding`, gehört zum T-0004-Fork. |
| M4 Kopplungsablauf | zurückgestellt | T-0004. |
| M5 Sackgassen, Repo-Pfad im Text | umgesetzt (ohne Onboarding) | Banner „Bridge kennt dieses iPhone nicht mehr“ führt in die Einstellungen; FRITZ!Box-Fehler bieten „Zu den Einstellungen“, „Erneut versuchen“ oder „Anleitung öffnen“ (Link auf `bridge/README.md`); Repo-Pfad aus dem Nutzertext entfernt. |
| M6 Lautsprecher nur per Route-Picker | umgesetzt | Ohne externe Ausgabe ist der Audio-Knopf ein Lautsprecher-Schalter (`overrideOutputAudioPort`); mit Kopfhörer, Bluetooth oder Auto öffnet er den System-Picker. `CallCenter` nicht angefasst (nur ein Text, siehe UX-Texte). |
| M7 Anruf nicht minimierbar | umgesetzt ab iOS 26.1 | Chevron oben links minimiert in eine `tabViewBottomAccessory`-Pille (Name, Timer, „Zurück zum Anruf“). `tabViewBottomAccessory(isEnabled:)` gibt es erst ab 26.1; auf 26.0 wird der Chevron gar nicht angezeigt, damit keine Sackgasse entsteht. |
| M8 Listen ohne Kontextmenü, Datum | umgesetzt | Gemeinsame `CallHistoryRow` für Anrufliste und FRITZ!Box; „Gestern“, Wochentag innerhalb von 7 Tagen, sonst `dd.MM.yy`; Kontextmenü „Anrufen“, „Nummer kopieren“, „Im Tastenfeld öffnen“; Kontakte mit „Anrufen“/„Nummer kopieren“. |
| M9 Glas-Tasten ohne Container | umgesetzt | `GlassEffectContainer` um Tastenfeld und Steuerung, `glassEffectID` für das Morphing. |
| M10 Watch-Tasten ohne Druck-Feedback | umgesetzt | `WatchPressStyle` (Scale 0,95, Deckkraft 0,7, Loslassen mit Ease-out) an allen Tasten, auch an der 0 mit Long-Press. |
| M11 Details in den Einstellungen | umgesetzt | Fußnote, wenn kein Push-Token da ist; Mikrofon-Zeile wird zur Aktion („Mikrofon erlauben“ bzw. „In Einstellungen erlauben“) und aktualisiert sich bei Rückkehr in die App. |
| M12 Tastenfeld auf dem SE läuft über | umgesetzt | `KeypadView`: Tastengröße 56–78 pt nach verfügbarer Höhe; der Anrufknopf folgt. Rechnung unten. |
| M13 Doppelte VoiceOver-Ausgabe | umgesetzt | Beschriftungen unter Annehmen/Ablehnen sind `accessibilityHidden`, die Knöpfe tragen die Labels selbst. |

### HIG „Sollte“

| Punkt | Status |
|---|---|
| Kontrast | umgesetzt (H3) |
| Watch-Tasten ≥ 40 pt | teilweise: 40 pt passen auf einer 41-mm-Watch nicht in 4 Zeilen (etwa 135 pt Höhe). Die Tasten füllen jetzt die Fläche (etwa 53 × 30 pt auf 41 mm, größer auf 45/46 mm). Steuerknöpfe im Anruf haben 40 pt (`WatchTheme.minTarget`), Löschen 32 × 32. |
| DTMF-Label | umgesetzt: „Noch keine Tastentöne“ bzw. die gesendeten Töne Ziffer für Ziffer |
| Scanner-Detent | T-0004-Fork |
| `interactiveDismissDisabled` | umgesetzt am Anruf-Cover |

### Animationen

| Nr. | Status | Umsetzung |
|---|---|---|
| 1 Stumm per Magic Replace | umgesetzt | `.contentTransition(.symbolEffect(.replace.magic(fallback: .replace)))` auf iPhone und Watch, Füllung per `motion(.snappy)`, Haptik `.selection` |
| 2 Statuszeile `blurReplace` | umgesetzt | `.id(StatusKey)` + `.transition(.blurReplace)`, leichte Haptik beim Verbinden (iPhone `.impact(weight: .light)`, Watch `.start`), Ansage für VoiceOver |
| 3 Filterwechsel in Listen | umgesetzt | `motion(.standard, value: filter)` bzw. `missedOnly`; Quellenwechsel per Überblendung |
| 4 Spinner im Kopplungs-Button | T-0004 | |
| 5 Glas-Morphing | umgesetzt | Mitte-Taste „5“ trägt dieselbe `glassEffectID` wie der Tastenfeld-Knopf; Stumm und Audio morphen mit |

Alle Animationen laufen über `View.motion(_:value:)` bzw. `withMotion`, die bei „Bewegung reduzieren“ auf eine Überblendung von 0,15 s zurückfallen.

### Feinschliff

| Punkt | Umsetzung |
|---|---|
| Token-Drift | `Theme.Space.hairline` (2 pt) statt freier `2`; `textPrimary`/`textSecondary` entfernt (System-Stile); Radien über `Theme.Radius` |
| Schriftskala | Text nur über System-Textstile bzw. `@ScaledMetric` |
| Tastenfeld-Timing | Hervorhebung sofort beim Drücken, Loslassen per Ease-out 0,25 s (`pressAnimation`) |
| Pressed-Scale 0,96 | `Theme.pressedScale`; Tastenfeld 0,97, Watch 0,95 |
| Glow 18 % + Ring | `CallActionButton`: Schatten 0,18, Ring aus der Füllfarbe 20 % dunkler, innerer Lichtrand |
| `.motion`-Helper | `View.motion(_:value:)`, `withMotion`, `pressAnimation` in `Theme.swift`; Watch-Pendant in `WatchTheme.swift` |
| `Text(timerInterval:)` | Anrufdauer auf iPhone, Watch und in der Pille |
| UX-Texte | „Kopple“ statt „Koppel“ (iPhone, Watch, `CallCenter`-Meldung); „Noch nicht bereit für Anrufe“ statt „Wartet auf Push-Freigabe“; Watch-Alert-Titel „Anruf nicht möglich“ |
| `guard` bei `removeLast` | Löschen auf iPhone und Watch |
| Watch: „Stumm“-Label | Label bleibt „Stumm“, der Zustand steckt in Symbol und Füllung |
| Watch: „Kopplung aufheben“ | eigene Unterseite „Kopplung“ mit Bestätigungsdialog statt Knopf auf der Startseite |

### App-Icon

Zurückgestellt. Es gibt keinen Icon-Generator im Repo. Dunkle und getönte Varianten sowie eine geschichtete Quelle brauchen Icon Composer bzw. Designarbeit. Ohne diese Werkzeuge sähen die Varianten schlechter aus als das jetzige Icon. Den Onboarding-Glyph (M3) passt der T-0004-Fork an.

## Kontraste (WCAG, gerechnet)

Text-Farben auf Weiß (hell) bzw. Schwarz (dunkel). Ziel ≥ 4,5:1.

| Farbe | Hell | Dunkel | Hell, erhöhter Kontrast | Dunkel, erhöhter Kontrast |
|---|---|---|---|---|
| `DangerText` | #C4262B 5,74 | #FF9592 9,97 | #A1161B 7,94 | #FFB8B5 12,78 |
| `WarningText` | #8A5A00 5,93 | #F2C464 12,86 | #6B4500 8,48 | #F8DA94 15,45 |
| `AccentText` | #0F766E 5,47 | #5EEAD4 14,20 | #0B5E57 7,63 | #99F6E4 16,66 |

Auf dem gruppierten Hintergrund #F2F2F7 (hell): `DangerText` 5,14, `WarningText` 5,31, `AccentText` 4,90.

Vorher: Rot #E5484D 3,91, Warnung #D29922 2,52, Akzent #0D9488 3,74.

Weiße Symbole auf Füllflächen (nicht-textliche Grafik, Ziel ≥ 3:1):

| Fläche | Standard | Erhöhter Kontrast |
|---|---|---|
| `Call` | #2EA043 3,37 | #1F7A31 5,40 |
| `Danger` | #E5484D 3,91 | #C4262B 5,74 |

`Warning` ist hell #8A5A00 und damit auch als Text sicher. Grund: `PairingView` (T-0004-Bereich) setzt `Theme.warning` als Textfarbe.

## Layout-Rechnung

Verfügbare Höhe = Bildschirm minus Safe Area (SE 20/0, 16 59/34, 16 Pro 62/34), im Tastenfeld-Tab zusätzlich minus Tab-Leiste 49. Werte in pt, geschätzt aus Standardgrößen; auf dem Gerät nicht gemessen.

**Tastenfeld-Tab:** fest 290 (Banner inkl. Abstand bis 64, zwei Spacer 2 × 24, Nummer 82, Zeilenabstand 3 × 16, Anrufzeile 16 + 32), dazu 5 Tastenhöhen. `k = min(78, max(56, (H − 290) / 5))`

| Gerät | H | k | Summe | Reserve |
|---|---|---|---|---|
| iPhone SE | 598 | 61,6 | 598 | 0 |
| iPhone 16 | 710 | 78 | 680 | 30 |
| iPhone 16 Pro | 729 | 78 | 680 | 49 |

**Anruf, Tastenfeld-Modus:** fest 398 (Leiste 44 + 8, kompakter Kopf ~92, Abstand 16, zwei Spacer 2 × 24, Tonzeile 42, Zeilenabstand 3 × 16, Auflegen 76, unten 24), dazu 4 Tastenhöhen. `k = min(74, max(52, (H − 398) / 4))`

| Gerät | H | k | Summe | Reserve |
|---|---|---|---|---|
| iPhone SE | 647 | 62,3 | 647 | 0 |
| iPhone 16 | 759 | 74 | 694 | 65 |
| iPhone 16 Pro | 778 | 74 | 694 | 84 |

Die erste Fassung vergaß die Minimieren-Leiste (52 pt) im Anruf-Budget. Auf dem SE wären es 694 pt bei 647 pt verfügbar gewesen. Behoben in `67d06957`, zusammen mit dem zweizeiligen Banner im Tab.

Offen: Bei großen Schriftgraden wächst der Kopf über die geschätzten 92 bzw. 82 pt. Die Tasten schrumpfen dann nicht mit, die Spacer fangen nur ihren Überschuss ab. Das muss am Gerät bei AX-Größen geprüft werden.

## Geänderte Pfade

- `ios/Housephone/Design/{Theme,Components}.swift`
- `ios/Housephone/Resources/Assets.xcassets/{Call,Danger,Warning,DangerText,WarningText,AccentText}.colorset`
- `ios/Housephone/App/{AppModel,RootView}.swift`, `ios/Housephone/Calling/CallCenter.swift` (ein Text)
- `ios/Housephone/Features/Call/InCallView.swift`, `ios/Housephone/Features/Keypad/{KeypadGrid,KeypadView}.swift`
- `ios/Housephone/Features/Recents/{CallHistoryRow (neu),RecentsView}.swift`, `ios/Housephone/Features/FritzBox/{FritzBoxHistoryList,FritzBoxContactsList,FritzBoxStatusViews}.swift`, `ios/Housephone/Features/Contacts/ContactsView.swift`
- `ios/Housephone/Features/Settings/{SettingsView,AcknowledgementsView}.swift`
- `ios/HousephoneWatch/{Design/WatchTheme,Calling/WatchCallCenter,Features/WatchRootView,WatchKeypadView,WatchInCallView,WatchContactsView}.swift`
- beide `Localizable.xcstrings`: Schlüssel ergänzt, umbenannt und ungenutzte entfernt (iPhone 5, Watch „Stumm aus“), ohne Umsortieren

## Abweichungen vom Auftrag

- `CallCenter.swift`: nur die Meldung „Kopple zuerst …“ geändert (UX-Text), keine Logik. Für M6 war `CallCenter` nicht nötig.
- `AppModel`: neues `isCallMinimized` und `canMinimizeCall` für M7.
- Entfernte Katalogschlüssel hatten 0 Verwendungen im Code (per `grep` geprüft).
