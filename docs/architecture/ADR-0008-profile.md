# ADR-0008: Mehrere Profile mit eigener Festnetznummer

- Status: vorgeschlagen (2026-10-01), Branch `feat/profiles` (HPHN-42)
- Ergänzt ADR-0001 (Bridge), ADR-0003 (Telefonbuch/Anrufliste), ADR-0007 (Koppeln im Heimnetz).

## Kontext

- Im Haushalt telefonieren mehrere Personen über dieselbe FRITZ!Box, jede mit eigener Festnetznummer aus demselben Vertrag (z. B. du und deine Mutter).
- Bisher ist die Bridge genau ein IP-Telefon: Jeder Anruf klingelt auf allen Geräten, alle wählen mit derselben Nummer, alle sehen dieselbe Anrufliste.

## Entscheidung

1. **Profil = eigenes IP-Telefon an der FRITZ!Box.** Welche Nummer es nach außen zeigt und auf welche es klingelt, stellt man in der FRITZ!Box am IP-Telefon ein. Die Bridge muss die Nummern dafür nicht kennen.
2. **Konfiguration:** Der `sip`-Block bleibt das Standardprofil (`id` `default`, Name aus `profile.name`, sonst „Standard“). Weitere Profile stehen unter `lines: [{id, name, sip: {username, password | passwordFile, bindPort?}, numbers, phonebooks}]`. Registrar, Bind-Adresse und RTP-Ports teilen sich alle Profile.
   - Höchstens 8 Profile; IDs `^[a-z0-9][a-z0-9-]{0,31}$`, eindeutig, `default` ist vergeben; SIP-Benutzer und lokale SIP-Ports eindeutig.
   - Home Assistant: `profile_name`, `profile_numbers` und die Liste `lines` mit `id`, `name`, `sip_username`, `sip_password`, `numbers` (kommagetrennt).
3. **Eine Registrierung pro Profil, jede auf eigenem SIP-Port** (`sip.bindPort + 1 + Position`, wenn nicht gesetzt). Die FRITZ!Box schickt einen Anruf an den Contact des IP-Telefons, das klingeln soll; die Bridge weiß also allein am Port, welches Profil gemeint ist, ohne SIP-Header auszuwerten.
4. **Jedes Gerät gehört genau einem Profil** (`profile` in `devices.json`; leer = Standardprofil, so bleiben bestehende Geräte ohne Migration im Standardprofil).
   - Das Profil wird beim Koppeln festgelegt: `pair -profile ID`, im Dashboard und in der TUI per Auswahl, bei der Freigabe aus dem Heimnetz (ADR-0007) per Auswahl bzw. `devices approve -profile ID`. Ohne Auswahl: Standardprofil; mit mehreren Profilen fragen CLI und TUI nach.
   - Eine Watch gehört immer zum Profil ihres iPhones (beim Koppeln aus dem Companion-Code abgeleitet, beim Verschieben des iPhones mitverschoben).
   - Verschieben: `devices move <id> <profil>`, TUI-Taste `p`, Dashboard. Ein verbundenes Gerät wird mit Close-Code 4004 getrennt, seine laufenden Anrufe enden; nach dem Neuverbinden bekommt es das `welcome` des neuen Profils.
5. **Durchsetzung auf der Bridge, nicht in der App:**
   - Eingehend: Der Anruf trägt das Profil seiner Leitung. Nur dessen Geräte bekommen `call.incoming` und VoIP-Push.
   - Anruf-IDs: Jede Anruf-Nachricht und `GET /v1/calls/{id}` prüft das Profil der Verbindung gegen das des Anrufs (auch nach dem Ende, über den Tombstone). Ein fremder Anruf verhält sich wie ein unbekannter. Vorher konnte jedes gekoppelte Gerät per `call.attach` mit bekannter ID an einen klingelnden Anruf.
   - Ausgehend: über die Leitung des eigenen Profils, also mit dessen Nummer.
   - Status: `status`-Nachrichten und `welcome.sipRegistered` beziehen sich auf die eigene Leitung.
   - `device.paired` geht nur an die anderen Geräte desselben Profils.
   - Das Profil einer WebSocket-Verbindung steht beim Aufbau fest; HTTPS-Anfragen lesen es bei jeder Anfrage neu.
6. **Anrufliste:** Mit mehreren Profilen sieht jedes nur die Einträge, deren eigene Seite (`Called` bei eingehenden, `Caller` bei ausgehenden Anrufen der FRITZ!Box-Liste) zu seinen `numbers` passt. Verglichen werden die Ziffern ohne führende Nullen, ein Endstück mit mindestens 5 Ziffern genügt, weil die FRITZ!Box eigene Nummern mal mit, mal ohne Vorwahl schreibt. Ein Profil ohne Nummern sieht dann keine Anrufliste (und bekommt das Feature nicht in `welcome`), denn seine Einträge lassen sich nicht von denen der anderen unterscheiden. Mit nur einem Profil bleibt alles ungefiltert.
7. **Telefonbuch:** Standardmäßig gemeinsam. `phonebooks` beschränkt ein Profil auf bestimmte FRITZ!Box-Telefonbücher; das gilt auch für den Anrufernamen bei eingehenden Anrufen. Abgerufen wird weiter einmal für alle (ein Cache).
8. **Gerät eines entfernten Profils** (aus der Konfiguration gestrichen): bekommt nichts (keine Anrufe, `403` auf Telefonbuch und Anrufliste, Wählen scheitert), bis es verschoben oder entfernt wird. Sicherer als ein stiller Rückfall ins Standardprofil.
9. **Verwaltung:** Admin-API `GET /v1/profiles`, `POST /v1/devices/{id}/move`, Profil bei `POST /v1/pairing` und `…/approve`. TUI: Profile mit Anmeldestatus in der Übersicht, Spalte „Profil“, Statistik pro Profil, Auswahl beim Koppeln, Freigeben und Verschieben. Dashboard: Profilkarte, Auswahl in denselben Formularen. Selbsttest: Anmeldung jedes weiteren Profils und Hinweis auf Profile ohne Nummern.

## Folgen

- Jede Person braucht ein eigenes IP-Telefon in der FRITZ!Box. Das automatische Anlegen per TR-064 (HPHN-25) gibt es bisher nur im Direktmodus der App.
- Der Admin bleibt eine Rolle für den ganzen Haushalt: Wer TUI, CLI oder das Dashboard bedienen darf, sieht alle Profile. Profilgrenzen gelten für Geräte, nicht für Admins.
- Die Anrufliste hängt davon ab, dass die Nummern in `numbers` so eingetragen sind, wie die FRITZ!Box sie in der Liste schreibt. Der Vergleich ist bewusst tolerant; ein falsch eingetragenes Profil sieht im Zweifel zu wenig, nicht fremde Anrufe.
- Mehr SIP-Registrierungen und UDP-Ports pro Bridge; im Docker-Compose-Betrieb und im Add-on (Host-Netz) ist dafür keine Portfreigabe nötig, beides läuft nur im LAN.

## Nicht umgesetzt

- Freigabe am Admin-iPhone mit Face ID (HPHN-37).
- Eigene Push-Topics oder Apps pro Profil: Alle Profile nutzen dieselbe App.
- Profilwahl in der App selbst; die App zeigt ihr Profil nur an.
