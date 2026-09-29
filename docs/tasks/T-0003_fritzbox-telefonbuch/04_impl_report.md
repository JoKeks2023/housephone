# Umsetzungsbericht T-0003

**Stand:** 2026-09-29 – PR #12 (Merge `803c44a0`)

## Bridge

- **`internal/fritzbox`:**
  - TR-064-Client: `GetSecurityPort` ohne Auth, danach HTTPS (Zertifikat bewusst ungeprüft) mit Digest-Auth auf `X_AVM-DE_OnTel`.
  - Abrufe: `GetPhonebookList`, `GetPhonebook`, `GetCallList` plus Download über die `sid`-URLs.
  - XML-Parser für Telefonbuch und Anrufliste, auch ISO-8859-1.
  - Zwischenspeicher: 10 min für das Telefonbuch, 30 s für die Anrufliste, gleichzeitige Abrufe gebündelt (singleflight).
  - ETag über den Inhalt.
  - Namenssuche in nationaler Form; mehrdeutig = kein Name.
- **HTTP:** `GET /v1/phonebook` (ETag/304), `GET /v1/history?limit=` (1–500), `503 fritzbox_unavailable`.
- **Signalisierung:** `welcome.features`; der Anrufername aus dem Telefonbuch geht in Push und `call.incoming`, ohne den Push zu verzögern.
- **Konfiguration:** `fritzbox.*`, Passwort per `HOUSEPHONE_FRITZBOX_PASSWORD(_FILE)`, Zonendaten eingebettet (`time/tzdata`). README Abschnitt 8.

## Apps

- **HousephoneKit:**
  - Modelle, `BridgeHTTPClient.phonebook(ifNoneMatch:)`/`history(limit:)`
  - `FritzBoxResource`: Cache auf der Platte, ab dem ersten Entsperren lesbar
  - `PhonebookNameIndex`
- **iPhone:**
  - Kontakte: „FRITZ!Box | iPhone“, Favoriten, A–Z, Suche, Nummernauswahl
  - Anrufe: „Housephone | FRITZ!Box“
  - Namens-Rückfall überall, auch in CallKit
- **Watch:** Kontakte aus der FRITZ!Box mit Favoriten und Suche; verpasste FRITZ!Box-Anrufe der letzten 24 h auf der Startseite.

## Abweichungen und Präzisierungen

- Leeres `device` wird weggelassen, wie `name`. Fixture und Spezifikation sind angepasst.
- Ein Kontakt ohne Namen bekommt seine erste Nummer als Namen.
- Fehlt `uniqueid`, lautet die ID `<pbid>-n<Position>`.
- Schlägt ein Abruf fehl, antwortet die Bridge mit 503 statt mit veralteten Daten. Die Apps zeigen dann ihren eigenen Cache mit Stand.
- `limit` außerhalb 1–500 → `400 bad_request`.
