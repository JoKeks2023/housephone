# Scope

## Drin

- **Bridge:**
  - TR-064-Client über HTTPS mit Digest-Auth: `GetSecurityPort`, `GetPhonebookList`, `GetPhonebook`, `GetCallList` plus Download und Parsen der XML-Dateien
  - Cache
  - `GET /v1/phonebook` (ETag/304), `GET /v1/history`
  - `welcome.features`
  - Anrufername aus dem Telefonbuch
  - Konfiguration `fritzbox.*` mit Passwort per Umgebungsvariable oder Datei
  - README-Abschnitt: FRITZ!Box-Benutzer anlegen, TR-064 erlauben
- **HousephoneKit:** Modelle, HTTPS-Client, Cache (ETag), Nummernabgleich für Namen, Tests gegen die HTTP-Fixtures
- **iPhone:**
  - Tab Kontakte: Umschalter „FRITZ!Box | iPhone“, Favoriten oben, Suche, Nummernauswahl
  - Tab Anrufe: Umschalter „Housephone | FRITZ!Box“ mit Richtung/Ergebnis, Gerät und Rückruf
  - Namen aus dem FRITZ!Box-Telefonbuch als Rückfall überall, wo eine Nummer angezeigt wird
  - Offline aus dem Cache
- **Watch:**
  - FRITZ!Box-Telefonbuch (Favoriten oben, Suche) zum Anrufen
  - verpasste Anrufe aus der FRITZ!Box-Anrufliste auf der Startseite
- `docs/setup.md` ergänzen

## Nicht drin

- Kontakte in der FRITZ!Box bearbeiten
- Kontaktbilder
- Anrufbeantworter-Nachrichten abspielen
- Rufsperren/Umleitungen
