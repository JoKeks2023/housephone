# ADR-0010: Push-Relay und Ende-zu-Ende-verschlüsselte Pushes

- Status: vorgeschlagen (2026-10-01), Branch `feat/push-relay` (HPHN-22)
- Ersetzt den eigenen APNs-Key als Normalfall (ADR-0001). Ergänzt ADR-0004 (Anmeldung v2) und ADR-0006 (Home-Assistant-Add-on).

## Kontext

- VoIP-Pushes an die App kann nur senden, wer einen APNs-Key des Developer-Accounts hat, der die App veröffentlicht. Nutzer der App-Store-App haben keinen und sollen keinen brauchen.
- Den Key in Bridge-Image oder Add-on zu legen scheidet aus: Er gilt für alle Apps des Teams und ließe sich aus jedem Download herausziehen. Wer ihn hat, kann allen Nutzern Pushes schicken; Apple sperrt ihn bei Missbrauch, dann klingelt bei niemandem mehr etwas.
- Vorgabe des Nutzers (2026-10-01): keine Dateien im eigenen Home Assistant ablegen müssen; der Relay läuft per Docker Compose auf dem eigenen VPS (netcup), kein Cloudflare Worker.
- Bisher gingen Nummer und Name des Anrufers im Klartext über Apple (ADR-0004, offene Punkte).

## Entscheidung

1. **Relay des Anbieters.** `housephone-relay` (Go) hält den APNs-Key und leitet weiter. Code und Betrieb (Docker Compose, gebaut auf dem VPS, erreichbar über einen Cloudflare Tunnel ohne offene Ports) liegen in einem **privaten Repository** des Anbieters, nicht hier (Vorgabe 2026-10-02). Dieses Repository enthält nur die Seite der Bridge (`internal/push`, `RelayRequest`). Das Relay speichert nichts auf Platte, braucht keine Datenbank und schreibt kein Zugriffslog.
2. **Schnittstelle.** `POST /v1/push` mit `{token, environment, topic, collapseId?, sealed}`:
   - `token`: APNs-Gerätetoken (hex, klein), `environment`: `production` oder `development`.
   - `topic` muss in `RELAY_TOPICS` stehen (Standard: App und Watch-App, je `….voip`), sonst `403`.
   - `sealed`: versiegelte Nutzlast (Punkt 3), base64url. Klartext lehnt der Relay ab (`400`). An APNs geht genau `{"sealed": "…"}` als VoIP-Push mit Priorität 10, `apns-expiration: 0`, `apns-collapse-id = collapseId`.
   - Antworten: `200`, `400`, `403`, `410` (Token ungültig, Bridge vergisst es), `413`, `429`, `502` (APNs-Fehler, mit `reason`).
3. **Versiegelte Nutzlast** (`internal/pushseal`, Vektoren in `docs/protocol/fixtures/crypto/push-vectors.json`):
   - Das Gerät erzeugt einen X25519-Schlüssel (**Push-Schlüssel**) und meldet den öffentlichen Teil als `pushKey` (base64url, 32 Byte) in `hello` und `device.update` (Signaling v1.3).
   - Pro Push: ephemerer X25519-Schlüssel `e`; `key = HKDF-SHA256(X25519(e, pushKey), salt = e.pub ‖ pushKey, info = "housephone-push-v1", 32)`; `box = ChaCha20-Poly1305(key, nonce = 0¹², aad = "housephone-push-v1", PushIncomingCall-JSON)`; `sealed = e.pub ‖ box`. Jeder Schlüssel wird nur einmal benutzt, deshalb ist die feste Nonce sicher.
   - Klartext ist dasselbe `PushIncomingCall` wie bisher (`push.incoming_call.json`).
4. **Bridge.** `apns.relay` (oder `HOUSEPHONE_PUSH_RELAY`):
   - leer = Relay der App-Store-App (`config.DefaultPushRelay`), `off` = aus, sonst eigene Relay-URL.
   - Ein eigener APNs-Key (`apns.keyFile`, `keyId`, `teamId`) hat Vorrang; dann sendet die Bridge direkt. Auch dann versiegelt sie für Geräte mit `pushKey`, ältere Apps bekommen Klartext.
   - Über den Relay gehen nur versiegelte Pushes. Geräte ohne `pushKey` bekommen dort keinen Push; der Selbsttest warnt („Push-Schlüssel“).
   - Status: `pushMode` (`apns`, `relay`, `off`) und `pushRelay`; `apnsConfigured` heißt jetzt „Push möglich“.
5. **Missbrauchsschutz.** Rate-Limit im Speicher: 10 Pushes pro Gerätetoken (dann einer alle 6 s) und 60 pro Absenderadresse (dann einer pro Sekunde); hinter dem Reverse Proxy zählt der letzte `X-Forwarded-For`-Eintrag. Nur erlaubte Topics, nur versiegelte Nutzlast bis 2 KB, Anfragen bis 8 KB.
6. **App.** Erzeugt den Push-Schlüssel einmal pro Installation (Schlüsselbund, `AfterFirstUnlockThisDeviceOnly`, ohne Biometrie, weil Pushes im Sperrbildschirm ankommen), meldet `pushKey` und öffnet im PushKit-Handler `{"sealed"}` vor dem Melden an CallKit. Klartext-Pushes (eigener Key, alte Bridge) bleiben lesbar. Scheitert das Öffnen, meldet sie wie bisher einen Anruf und beendet ihn sofort (PushKit-Pflicht).

## Sicherheitsbetrachtung

- **Relay-Betreiber und Apple** sehen Gerätetoken, Topic, Zeitpunkt und Absenderadresse der Bridge, aber weder Nummer noch Name.
- **Wer ein Gerätetoken kennt** (z. B. eine kompromittierte Bridge), kann dem Gerät über den Relay Pushes schicken. Ohne den Push-Schlüssel nur unlesbare: Die App meldet sie als gescheiterten Anruf und beendet sie sofort. Das Rate-Limit begrenzt das Stören. Gerätetokens bekommt nur die Bridge, mit der das Gerät gekoppelt ist.
- **Wiedereinspielen** einer mitgeschnittenen Nutzlast lässt das Telefon kurz klingeln; die App fragt die Bridge nach dem Anruf (`call.attach`), findet ihn nicht und beendet ihn. Kein Zeitstempel in der Nutzlast, damit das Format zum bestehenden `PushIncomingCall` passt.
- **Der APNs-Key** liegt nur auf dem Relay-Server, als Docker-Secret, lesbar für den Container-Nutzer; das Verzeichnis `secrets/` ist nur für root (700).
- **Ausfall des Relays:** Pushes fallen aus, verbundene Apps klingeln weiter über die WebSocket-Verbindung. Wer das nicht will, betreibt einen eigenen Relay oder trägt einen eigenen Key ein.

## Verworfen

- **Key im Image oder Add-on:** siehe Kontext.
- **Cloudflare Worker:** Kontingent des Accounts teilt sich mit anderen Seiten des Nutzers, eigener Account keine Option; HTTP/2 zu APNs aus Workers ungeprüft.
- **Relay-Token pro Gerät** (App registriert ihr Token beim Relay und gibt der Bridge nur ein verschlüsseltes Token): schützt nur das rohe Gerätetoken vor der Bridge, die ohnehin pushen darf; kostet eine zusätzliche Verbindung der App zum Anbieter. Nachrüstbar.
- **Local Push Connectivity** statt Relay: wirkt nur in festgelegten WLANs und braucht ein Entitlement auf Antrag (ADR-0005). Bleibt eine Ergänzung für den Modus ohne Bridge.

## Folgen

- Die Bridge braucht keinen APNs-Key mehr; das Add-on verliert die APNs-Felder und bekommt optional `push_relay`.
- `docs/veroeffentlichung.md`: Datenschutzangaben anpassen (Anbieter verarbeitet Push-Tokens; Cloudflare als Auftragsverarbeiter des Tunnels sieht Token, Topic und Adresse der Bridge, nicht Nummer oder Name), Relay-Domain `housephone.relay.jorisconrad.com` (`config.DefaultPushRelay`).
- Selbst gebaute Apps mit anderem Bundle: eigener APNs-Key (oder ein eigenes Relay mit der Schnittstelle aus Punkt 2).
