# Review 2026-09-29: Design, HIG, App Store

**Stand:** `master` 599cde64
**Art:** drei unabhängige, nur lesende Prüfungen:
- HIG-Reviewer
- App-Store-Reviewer
- Design-Audit mit den Skills `jorisconrad-design-language`, `apple-skills:design`, `apple:visual-qa`, `apple:accessibility`, `emil-design-eng`, `find-animation-opportunities`

Nichts davon lief zur Laufzeit; alle Befunde stammen aus dem Code.
Umsetzung: T-0007. Kopplungs-UI: T-0004.

## 1. „Zu nah am Upstream?“ – Nein

| Prüfung | Ergebnis |
|---|---|
| Telephone-Bezeichner in `ios/` und `bridge/` (`AKSIP`, `64 Characters`, `PJSIP`, `com.tlphn`, GPL-Header) | 0 Treffer. In `bridge/` taucht nur das Protokollwort `telephone-event` (RFC 4733) auf |
| Inhaltsgleiche Dateien, alt ↔ neu (648 ↔ 155 Dateien) | 2, beide die Xcode-Standarddatei `Assets.xcassets/Contents.json` |
| Upstream-Commits in `ios/` und `bridge/` | 0 von 49 |
| Funktion | Telephone ist ein macOS-SIP-Softphone; Housephone ist eine iOS-/watchOS-App mit eigener Bridge für die FRITZ!Box |

Kein Copycat- bzw. Plagiatsrisiko nach den Richtlinien 4.1, 4.3 und 5.2. Offen ist nur die **Lizenz-Herkunft des Repos** (GPL-3.0, alter macOS-Code im selben Repo). Empfehlung: vor dem Release ein eigenes Repo ohne Telephone-Historie mit eigener Lizenz.

## 2. App Store – was vor einer Einreichung fehlt

| Risiko | Richtlinie | Maßnahme |
|---|---|---|
| Hoch: Prüfer haben keine FRITZ!Box | 2.1 | Demo-Bridge mit Testanschluss plus Review-Notizen |
| Hoch: APNs-Key gehört dem Anbieter | – | Push-Relay |
| Mittel: GPL-Herkunft | 5.2 | eigenes Repo, eigene Lizenz |
| Mittel: `ITSAppUsesNonExemptEncryption: false` | Export | bewusst prüfen und bestätigen |
| Niedrig: Datenschutzerklärung | 5.1.1 | veröffentlichen, URL in ASC |
| Niedrig: Marke „FRITZ!Box“ | 5.2 | nur beschreibend, nicht im Titel oder Icon |
| Prüfen: `aps-environment` | – | Automatisches Signing setzt beim Export vermutlich `production`; beim ersten Archiv kontrollieren |

**Bereits korrekt:**
- Jeder VoIP-Push wird an CallKit gemeldet, auch in Sonderfällen.
- Hintergrundmodi, Purpose-Strings und Privacy Manifest sind vorhanden.
- Lizenzen der Drittkomponenten sind eingebettet.

## 3. HIG – keine Muss-Befunde, „bereit bzw. sehr nah dran“

**Sollte:**
- App-Icon als mehrschichtiges Icon-Composer-Icon
- Statusfarben ohne Pfad für „Erhöhter Kontrast“
- Watch-Tasten 30 pt statt ≥ 40 pt
- Anzeige der Tastentöne ohne VoiceOver-Label
- Scanner-Sheet ohne `.large`

**Kann:**
- Anrufbildschirm mit `.interactiveDismissDisabled(true)`
- Rückfrage vor „Alle Einträge löschen“
- Umschalter einheitlich platzieren

## 4. Design-Audit – etwa 75–80 % des Niveaus einer Apple-App

**Hoch:**
- H1: Das Tastenfeld im Anruf läuft auf iPhone SE und 16 über.
- H2: „Alles löschen“ ohne Rückfrage.
- H3: Im Light Mode liegt der Kontrast unter AA (Rot 3,91:1, Warnung 2,52:1, Akzenttext 3,74:1).
- H4: Der Wählpfad ist mit VoiceOver und Voice Control lückenhaft (Nummer wird als Zahl vorgelesen, „+“ und „Löschen“ nur per Geste).
- H5: Dynamic Type ist an den prominentesten Stellen eingefroren (30, 38 und 40 pt fest).

**Mittel:**
- M1: Umschalter uneinheitlich.
- M2: Der Akzent wird dekorativ verbraucht (Avatare, Zeilensymbole).
- M3: Das Onboarding-Glyph weicht vom Icon ab.
- M4: Kopplungsablauf (Spinner, „Abbrechen“ nach Erfolg, Fehler-Haptik), wird in T-0004 behoben.
- M5: Sackgassen ohne Aktion, Repo-Pfad im Nutzertext.
- M6: Lautsprecher nur über den Route-Picker.
- M7: Der Anruf lässt sich nicht minimieren.
- M8: Listen ohne Kontextmenü, „Gestern“ und Wochentage.
- M9: Glas-Tasten ohne Container.
- M10: Watch-Tasten ohne Druck-Feedback.
- M11: Details in den Einstellungen.
- M12: Das Tastenfeld auf dem SE läuft über.
- M13: Doppelte VoiceOver-Ausgabe bei eingehendem Anruf.

**Niedrig:**
- Token-Drift, Schriftskala, UX-Texte („Kopple“)
- Timing des Tastenfelds (Hervorhebung sofort, Ausblenden 0,25 s)
- Pressed-Scale 0,96

**Animationen mit Hebelwirkung:**
- Stummschalten per Magic Replace und Überblendung
- Statuszeile per `blurReplace` mit Haptik beim Verbinden
- Filterwechsel in den Listen
- Spinner im Kopplungs-Button
- Glas-Morphing zwischen Steuerung und Tastenfeld

Bewusst verworfen: Tab-Wechsel, gestaffeltes Onboarding, Verzögerung nach dem QR-Treffer.

**Top 5:**
1. Anrufbildschirm robust und lebendig machen
2. Farben als Asset-Farben mit AA-sicheren Varianten, neutrale Avatare
3. Dynamic Type und VoiceOver auf dem Wählpfad
4. Listen wie in der Telefon-App
5. Sackgassen schließen, Anruf-Pille

**Gut:** Tokens entsprechen der Designsprache; Reduce Motion ist zentral berücksichtigt; Status als Punkt plus Text; Tastenfeld mit Details auf Telefon-App-Niveau; Liquid Glass überwiegend korrekt; ruhige Datenzustände; Du-Form und saubere Typografie; Digital Crown auf der Watch.

## Manuelle Prüfliste (auf dem Gerät)

- VoiceOver: Wählpfad, Annehmen und Ablehnen, Stumm, Audio, Kopplung
- Voice Control: „Tippe Audio/Stumm/5“
- Schrift 200 % und AX5
- Erhöhter Kontrast in Hell und Dunkel
- Bewegung reduzieren
- Layout auf iPhone SE, 16 und 16 Pro
