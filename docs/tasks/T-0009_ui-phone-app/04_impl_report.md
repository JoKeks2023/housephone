# Umsetzungsbericht

Branch `feat/ui-phone-app`, Commits `66259a4c` (Kataloge im Xcode-Format) und `9fa44fe9` (Oberfläche).

## Pro Bildschirm

| Bildschirm | Änderung | Dateien |
|---|---|---|
| Tastenfeld | Glas-Statuskarte statt Banner: Leitung bereit/gestört, über Bridge oder FRITZ!Box; bei Problemen führt sie in die Einstellungen. Daneben „N verpasst“ (öffnet Anrufe → Verpasst). Vorschlag unter der Nummer aus Favoriten, letzten Anrufen und Kontakten, mit „+N“. | `Features/Keypad/ConnectionStatusCard.swift`, `KeypadView.swift` |
| Anrufe | Info-Button pro Zeile → Detail-Sheet (alle Anrufe mit der Nummer; Anrufen, Tastenfeld, Kopieren, Favorit). Kontaktfotos. Leer-Zustände mit Aktion („Zum Tastenfeld“, im Direktmodus ohne TR-064 „TR-064 aktivieren“). Badge am Tab für neue verpasste Anrufe. | `Features/Recents/*`, `App/RootView.swift`, `App/AppModel.swift` |
| Kontakte | Favoriten als Kacheln oben (iPhone- und FRITZ!Box-Liste), per Kontextmenü pro Nummer hinzufügen/entfernen, gespeichert nur auf dem iPhone (`UserDefaults`). Fotos aus den Kontakten. Der FRITZ!Box-eigene Abschnitt heißt jetzt „FRITZ!Box-Favoriten“. | `Features/Contacts/*`, `Data/FavoritesStore.swift`, `Features/FritzBox/FritzBoxContactsList.swift` |
| Einstellungen | Übersichtskarte oben: Name von Bridge bzw. FRITZ!Box, Modus, Leitung, „Unterwegs“, Apple Watch. | `Features/Settings/SettingsOverviewCard.swift`, `SettingsView.swift` |
| Anruf-Screen | Foto des Anrufers groß (128 pt), Hintergrund aus dem unscharfen Foto; ohne Foto ein Schein in der Farbe des Anrufers. Mit „Transparenz reduzieren“ kein Foto-Hintergrund. | `Features/Call/InCallView.swift` |
| Gemeinsam | Monogramm-Avatare in einer festen Identitätsfarbe pro Name (FNV-Hash, über Starts stabil); ruhiger App-Hintergrund mit Akzent-Schein oben; neu gestaltete Leer-Zustände. | `Design/Avatar.swift`, `Data/ContactsDirectory.swift` |

`ContactsDirectory` lädt jetzt die Vorschaubilder der Kontakte mit, das Vollbild nur auf Abruf für den Anruf-Screen.

## Kontaktposter

- Eine öffentliche API, mit der eine App das Kontaktposter lesen oder anzeigen kann, gibt es nicht. `CNContact` liefert nur `imageData` und `thumbnailImageData`. Deshalb nutzt die App das Kontaktfoto.
- Apple hat zu iOS 17 angekündigt: „Contact Posters will also be available for third-party calling apps.“ (Apple Newsroom, Juni 2023). Gemeint ist die System-Oberfläche von CallKit.
- Housephone meldet Anrufe mit `CXHandle(type: .phoneNumber, …)`. Damit kann iOS den Kontakt zuordnen und beim eingehenden Anruf das Poster selbst zeigen.
- Nicht überprüft: ob iOS das bei Housephone tatsächlich tut.

## Abweichungen

- Die Kataloge sind jetzt im Xcode-Format: `" : "`, Schlüssel in `localizedStandardCompare`-Reihenfolge, kein Zeilenumbruch am Ende. Der Formatierer wurde byte-genau gegen von Xcode geschriebene Dateien geprüft.
- Vier Texte des alten Banners sind als `stale` markiert.
