# Validierungsbericht

| Prüfung | Ergebnis | Beleg |
|---|---|---|
| Kit-Tests | 214 Tests grün, 6 Läufe hintereinander ohne Ausreißer | `swift test` lokal |
| #33 unter Last | Test grün mit 25 ms Verzögerung vor jeder Antwort (vorher zählte er dann ≥ 3 REGISTER) | lokaler Versuch, nicht eingecheckt |
| Parser an echter FRITZ!Box | `tr64desc.xml` einer 6591 Cable mit FRITZ!OS 8.25 → „FRITZ!Box 6591 Cable“, „8.25“, X_VoIP vorhanden | lokaler Test gegen die heruntergeladene Datei, nicht eingecheckt |
| Aktionen vorhanden | `X_AVM-DE_GetNumbers`, `GetClients`, `SetClient4`, `X_AVM-DE_Auth` `SetConfig`/`GetState` im SCPD | Abruf ohne Anmeldung |
| App-Build (unsigniert) und Kit-Tests in CI | grün, keine Warnungen in den neuen Dateien | CI-Lauf 36834846733 |

## Nicht verifiziert

- Anlegen des IP-Telefons an einer echten FRITZ!Box (bewusst keine schreibende Anfrage), auch nicht das genaue Format der eingehenden Nummern bei Auswahl einer einzelnen Nummer.
- Die Zwei-Faktor-Bestätigung an einer echten Box.
- Die Suche im echten WLAN (Gateways aus `NWPathMonitor`, ATS-Ausnahme für `fritz.box`).
- Die Oberfläche selbst: kein Simulator, kein Gerät.
