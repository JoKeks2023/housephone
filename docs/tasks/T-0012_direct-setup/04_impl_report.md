# Umsetzungsbericht

## HousephoneKit

| Datei | Inhalt |
|---|---|
| `FritzBoxDiscovery.swift` | Kandidaten `fritz.box` (aufgelöst) → WLAN-Gateways → `192.168.178.1`, nur private IPv4, Duplikate einmal. Prüfung über `http://<ip>:49000/tr64desc.xml`: Hersteller FRITZ!/AVM, Modell beginnt mit „FRITZ!“, Version aus `systemVersion/Display` („161.08.25“ → „8.25“), `X_VoIP` vorhanden. |
| `FritzBoxVoIP.swift` | `X_AVM-DE_GetNumbers`, `X_AVM-DE_GetClients`, `X_AVM-DE_SetClient4`; `X_AVM-DE_Auth` `SetConfig(start/stop)` und `GetState` mit Token im SOAP-Header. `FritzBoxPhoneSetup`: freier Platz (0–9), Benutzername `housephone`, `housephone2`, …, Kennwort 32 Zeichen aus dem erlaubten Alphabet, Wiedererkennen per Client-ID oder Name, Ablauf 866 → Bestätigung → erneut schreiben mit Token. |
| `TR064.swift` | `TR064Error` neu: `notAllowed` (606, vorher als `authentication` gemeldet), `secondFactorRequired/Blocked/Busy` (866–868). Envelope mit optionalem Token-Header. |

Quellen (FRITZ!, fritz.support/resources): „TR-064 Support – X_VoIP“ v71 (2025-08-08), „TR-064 Support – Authentication“ v5 (2025-08-07), „TR-064 First Steps“ (Rechte, Fehlercodes). Gegengeprüft am SCPD einer FRITZ!Box 6591 mit FRITZ!OS 8.25 (ohne Anmeldung): alle genutzten Aktionen vorhanden, Control-URLs `/upnp/control/x_voip` und `/upnp/control/x_auth`.

Format eingehender Nummern: laut Spezifikation „ähnlich“ `X_AVM-DE_GetNumbers` (`<List><Item>…</Item></List>`); leer heißt „klingelt bei allen Nummern“. Voreinstellung in der App ist „Allen Nummern“ und „Automatisch“ (leer = erste Nummer), beides ausdrücklich so dokumentiert.

## App

- `Direct/FritzBoxFinder.swift`: Gateways über `NWPathMonitor` (`path.gateways`), dann `FritzBoxDiscovery`.
- `Direct/DirectSetupModel.swift`: Suche, Bestandsaufnahme, Anlegen mit Bestätigung, Fehlermeldungen.
- `Features/Direct/DirectSetupView.swift`: Karte „FRITZ!Box … gefunden“, Auswahl „Automatisch | Manuell“, Anmeldung, „Weiterverwenden | Neu anlegen“, Auswahl der Nummern, Blatt „Bestätige an der FRITZ!Box“ (Taste drücken oder Code wählen). Neue Einrichtung startet automatisch, Bearbeiten startet manuell.
- `DirectConfiguration.fritzBoxClientID` (optional, alte Einträge bleiben lesbar).
- `Info.plist`: ATS-Ausnahme nur für `fritz.box` (HTTP). `NSAllowsLocalNetworking` deckt nur unqualifizierte Namen, `.local` und IP-Adressen; die Abfrage des TR-064-Sicherheitsports an `fritz.box` wäre sonst blockiert.
- 36 neue Texte Deutsch/Englisch.

## #33

Ursache: In den Tests ist T1 = 10 ms. Antwortet der Test später als 10 ms (auf einem ausgelasteten CI-Runner), wiederholt der User Agent den REGISTER (Timer E). Der Test zählte diese Wiederholung als dritten REGISTER, und `nextRequest` konnte sogar die Wiederholung statt der nächsten Transaktion liefern. Lösung im Test-Transport: `next` überspringt Wiederholungen einer schon gelieferten Transaktion (ACKs ausgenommen, die sind eigene Antworten); der Test zählt Transaktionen statt Datagramme.

## Folgeaufgaben

- Die Bridge könnte ihr IP-Telefon genauso selbst anlegen (Go, `bridge/internal/fritzbox`).
