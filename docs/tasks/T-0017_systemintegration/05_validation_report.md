# Validierung

## Phase 1

| Prüfung | Ergebnis |
|---|---|
| `swift test` (HousephoneKit, lokal) | siehe PR |
| CI: App-Build unsigniert, Siri-/Intents-Prüfung | siehe PR |
| Gerät: „Hey Siri, ruf <Favorit> mit Housephone an“ | offen |
| Gerät: „Hey Siri, ruf <Name nur im FRITZ!Box-Telefonbuch> mit Housephone an“ | offen |
| Gerät: Wahlwiederholung per Siri | offen |
| Gerät: Kurzbefehle-App zeigt die vier Aktionen, „Favorit anrufen“ ruft an | offen |
| Gerät: Langes Drücken aufs App-Icon zeigt Favoriten, Tippen ruft an (auch bei beendeter App) | offen |
| Gerät: `housephone://call?number=…` aus Safari fragt nach | offen |
| Gerät: Signieren mit App Group und Siri (automatische Signierung legt beides an) | offen |

Ohne die Gerätetests ist Phase 1 **nicht vollständig verifiziert**.

## Phase 2

| Prüfung | Ergebnis |
|---|---|
| CI: App-Build mit eingebetteter Widget-Extension | siehe PR |
| Gerät: Signieren der Extension (automatische Signierung, App Group) | offen |
| Gerät: Favoriten-Widget klein/mittel, Favoriten wählen, Tippen ruft an | offen |
| Gerät: Anrufe-Widget, Rückruf per Tippen, verpasste rot | offen |
| Gerät: Sperrbildschirm-Widget zeigt neue verpasste Anrufe; nach Ansehen der Liste 0 | offen |
| Gerät: Kontrollzentrum „Favorit anrufen“ und „Tastenfeld öffnen“, auch auf dem Action-Button | offen |
| Gerät: StandBy mit kleinem Favoriten-Widget | offen |

Ohne die Gerätetests ist Phase 2 **nicht vollständig verifiziert**.

## Phase 3

| Prüfung | Ergebnis |
|---|---|
| `swift test` (CompanionFavorites) | siehe PR |
| CI: Komplikationen eingebettet, Watch-App mit App-Intents-Metadaten | siehe PR |
| Gerät: Favorit am iPhone hinzufügen → erscheint auf der Watch (auch bei geschlossener Watch-App) | offen |
| Gerät: Komplikation „Favorit anrufen“ im Zifferblatt-Editor, Tippen ruft an | offen |
| Gerät: Komplikation „Verpasste Anrufe“ zählt, fällt nach 24 Stunden weg | offen |
| Gerät: „Hey Siri, ruf <Favorit> mit Housephone an“ an der Watch | offen |

Ohne die Gerätetests ist Phase 3 **nicht vollständig verifiziert**.

## Phase 4

| Prüfung | Ergebnis |
|---|---|
| CI: Build ohne CarPlay-Entitlement, Schalter, Entitlement-Gleichstand, Szene in der Info.plist | siehe PR |
| Apple: Entitlement „CarPlay Communication“ beantragt | offen (User) |
| Gerät/Auto nach Freigabe: Tabs, Anruf per Tippen, Nummernwahl, Fehlerhinweis, eingehender Anruf im CarPlay-Anrufbildschirm | offen |

Ohne Freigabe und Test im Auto ist Phase 4 **nicht verifiziert**.
