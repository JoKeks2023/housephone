# ADR-0003: Telefonbuch und Anrufliste der FRITZ!Box

- Status: angenommen (2026-09-29)
- Baut auf ADR-0001/0002 auf.

## Kontext

- Die FRITZ!Box führt das Telefonbuch des Haushalts und die Anrufliste des Anschlusses. Das umfasst auch Anrufe, die am Schnurlostelefon oder vom Anrufbeantworter angenommen wurden.
- Beides soll in der iPhone- und der Watch-App erscheinen. Zusätzlich soll ein eingehender Anruf den Namen aus dem FRITZ!Box-Telefonbuch tragen.
- Die FRITZ!Box bietet dafür TR-064, Dienst `urn:dslforum-org:service:X_AVM-DE_OnTel:1` unter `/upnp/control/x_contact`, laut AVM-Doku `x_contactSCPD` v41.
  - `GetPhonebookList`, `GetPhonebook` und `GetCallList` liefern je eine Download-URL (XML).
  - Nötig ist ein FRITZ!Box-Benutzer mit dem Recht **„Phone“**. In der Oberfläche heißt es „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“.
  - Die Anmeldung läuft per HTTP-Digest.
- Die Geräte erreichen die FRITZ!Box unterwegs nicht. Die Watch darf außerhalb eines Anrufs nur HTTPS sprechen.

## Entscheidung

1. **Die Bridge ist der einzige TR-064-Client.** Die FRITZ!Box-Zugangsdaten liegen nur auf dem Server.
2. **Die Geräte holen die Daten per HTTPS von der Bridge** (Bearer-Auth wie `/v1/device`):
   - `GET /v1/phonebook` liefert alle Telefonbücher zusammengeführt, mit ETag bzw. `304`.
   - `GET /v1/history` liefert die Anrufliste des Anschlusses, normalisiert.
   - Das Format steht in `docs/protocol/signaling-v1.md`, „Erweiterung v1.2“.
3. **TR-064 läuft verschlüsselt** über HTTPS auf dem Security-Port der Box (`GetSecurityPort`, meist 49443).
   - Das Zertifikat der FRITZ!Box ist selbstsigniert und wird nicht geprüft; geschützt wird gegen passives Mitlesen im LAN.
   - Digest-Auth kommt obendrauf.
4. **Cache in der Bridge:**
   - Telefonbuch: 10 min, ETag über den Inhalt; Invalidierung über den `timestamp` der FRITZ!Box.
   - Anrufliste: 30 s.
   - Die FRITZ!Box wird also auch bei vielen Geräten kaum belastet.
5. **Anrufername:** Liefert das SIP-`From` der FRITZ!Box keinen Anzeigenamen, sucht die Bridge die Nummer im gecachten Telefonbuch. Das Ergebnis landet im Push (`callerName`) und in `call.incoming`.
6. **Fähigkeiten:** `welcome.features` meldet `fritzbox.phonebook` und `fritzbox.history`, wenn TR-064 konfiguriert ist und funktioniert. Die Apps blenden die Ansichten sonst aus.
7. **Zeiten:** Die FRITZ!Box liefert Ortszeit (`TT.MM.JJ HH:MM`). Die Bridge rechnet mit `fritzbox.timezone` (Standard `Europe/Berlin`) in UTC um.

## Verworfene Alternativen

| Alternative | Warum nicht |
|---|---|
| Geräte sprechen selbst TR-064 | Geht unterwegs nicht, und die FRITZ!Box-Zugangsdaten lägen auf jedem Gerät. |
| Anrufliste aus dem Anrufmonitor (TCP 1012) | Enthält keine Historie und zeigt nur, was während der Laufzeit passiert. |
| Telefonbuch per Kontakt-Sync (CardDAV) ins iPhone | Mehr Infrastruktur; Kontakte würden dupliziert. |
