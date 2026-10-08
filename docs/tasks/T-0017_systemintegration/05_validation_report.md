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
