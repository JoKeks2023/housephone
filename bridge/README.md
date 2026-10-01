# Housephone Bridge

Die Bridge verbindet deine FRITZ!Box mit der Housephone-App. Sie meldet sich an der FRITZ!Box als ganz normales IP-Telefon an. Kommt ein Anruf, weckt sie iPhone und Apple Watch per VoIP-Push. Dann reicht sie das Gespräch durch – zum iPhone über WebRTC, zur Watch über die WebSocket-Verbindung. Das klappt zu Hause und unterwegs, ohne VPN auf dem Telefon.

Architektur: [`ADR-0001`](../docs/architecture/ADR-0001-bridge-architektur.md) (iPhone) und [`ADR-0002`](../docs/architecture/ADR-0002-watch.md) (Watch) · Protokoll: [`docs/protocol/signaling-v1.md`](../docs/protocol/signaling-v1.md)

```
FRITZ!Box ◄─SIP/RTP (LAN)─► Bridge ◄── WSS (Cloudflare Tunnel) ──► App
                              │  ◄════ Ton: UDP 50000 (Portfreigabe) ════►
                              └─ VoIP-Push (APNs) ──► Apple ──► App
```

## Voraussetzungen

- Ein Server im Heimnetz, der immer läuft (Linux mit Docker, oder direkt das Go-Binary).
- FRITZ!Box mit Telefonie (getestet wird gegen FRITZ!OS 8.x).
- Apple-Developer-Account (für den APNs-Key).
- Für unterwegs:
  - ein Cloudflare-Account mit Domain (für den Tunnel) oder alternativ eine TCP-Portfreigabe mit eigenem TLS,
  - eine UDP-Portfreigabe für den Ton.

## 1. IP-Telefon in der FRITZ!Box anlegen

1. `http://fritz.box` → **Telefonie → Telefoniegeräte → Neues Gerät einrichten**.
2. **Telefon (mit und ohne Anrufbeantworter)** → **LAN/WLAN (IP-Telefon)** → Name z. B. `Housephone`.
3. Benutzername (z. B. `620`) und ein **langes** Kennwort vergeben und notieren.
4. Rufnummer für ausgehende Anrufe wählen und festlegen, auf welche Nummern das Gerät reagiert.
5. **„Anmeldung aus dem Internet erlauben“ nicht aktivieren** – die Bridge steht im Heimnetz.

## 2. Portfreigabe für den Ton

**Internet → Freigaben → Portfreigaben → Gerät für Freigaben hinzufügen** → deinen Server wählen → **Neue Freigabe** → *Andere Anwendung*:

| Protokoll | Port an Gerät | bis Port | Port extern gewünscht |
|---|---|---|---|
| UDP | 50000 | 50000 | 50000 |

Mehr muss nicht offen sein. SIP (5060/5062) bleibt im LAN.

Die Bridge ermittelt ihre öffentliche IPv4 automatisch: Sie fragt alle 30 s die FRITZ!Box per UPnP. Ein IP-Wechsel bei dynamischer IP fällt so sofort auf, ohne Internet-Abfrage. Antwortet die Box nicht (UPnP-Statusinformationen aus), nimmt sie STUN, höchstens alle 10 min. Alternativ kannst du `media.publicIp` (feste IP) oder `media.publicHost` (z. B. deine MyFRITZ!-Adresse) setzen.

## 3. APNs-Key erstellen

1. [developer.apple.com](https://developer.apple.com/account/resources/authkeys/list) → **Certificates, Identifiers & Profiles → Keys → +**.
2. Namen vergeben und **Apple Push Notifications service (APNs)** ankreuzen, dann registrieren.
3. `AuthKey_XXXXXXXXXX.p8` herunterladen – **das geht nur einmal** – und die **Key ID** notieren.
4. Team-ID: `T9CA6D7T8N`, Topic: `com.jorisconrad.housephone.voip` (Standardwerte in `config.example.yaml`). Mit eigenem Team und Präfix (`Housephone einrichten.command`): deine Team-ID und `<präfix>.housephone.voip`; das Skript zeigt beide Werte am Ende an.

Welches APNs-Environment ein Gerät braucht, meldet die App selbst:
- Aus Xcode installierte Debug-Builds nutzen `development` (Sandbox).
- TestFlight- und App-Store-Builds nutzen `production`.

## 4. Cloudflare Tunnel für die Signalisierung

1. [Cloudflare Zero Trust](https://one.dash.cloudflare.com) → **Networks → Tunnels → Create a tunnel** → *Cloudflared* → Token kopieren.
2. **Public Hostname** hinzufügen:
   - Hostname z. B. `phone.deine-domain.de`
   - Service `HTTP` → `localhost:8080`
3. WebSockets funktionieren ohne weitere Einstellung. In `config.yaml`: `publicUrl: "wss://phone.deine-domain.de/v1/ws"`.

Der Tunnel trägt nur die Signalisierung (TCP/WebSocket). Der Ton läuft über die UDP-Freigabe aus Schritt 2, weil Cloudflare Tunnel kein UDP für öffentliche Hostnamen weiterleitet.

**Ohne Cloudflare:** Ein Reverse Proxy mit TLS (z. B. Caddy) auf Port 443 → `localhost:8080`, dazu eine TCP-Portfreigabe 443. `trustProxyHeaders` nur aktivieren, wenn der Proxy `X-Forwarded-For` setzt.
- Die Bridge glaubt diesen Headern nur bei Anfragen von localhost.
- Läuft der Proxy auf einem anderen Rechner, trag dessen IP unter `bridge.trustedProxies` ein und öffne `bridge.listen` für ihn.

`bridge.listen` steht im Beispiel auf `127.0.0.1:8080`. Die Bridge ist damit nur über den Tunnel erreichbar und nicht im ganzen LAN.

## 5. Starten (Docker Compose)

```sh
cd bridge
cp config.example.yaml config.yaml          # registrar (IP der FRITZ!Box), username, publicUrl anpassen
cp docker-compose.example.yml docker-compose.yml
mkdir -p data secrets
printf '%s' 'SIP-KENNWORT' > secrets/sip_password
cp ~/Downloads/AuthKey_XXXXXXXXXX.p8 secrets/apns_key.p8
chmod 600 secrets/*
# In docker-compose.yml: HOUSEPHONE_APNS_KEY_ID und user: "$(id -u):$(id -g)" eintragen
CLOUDFLARE_TUNNEL_TOKEN=... docker compose up -d
docker compose logs -f housephone-bridge    # erwartet: "registered at FRITZ!Box"
curl -s http://localhost:8080/v1/health     # {"status":"ok"} (Details nur für gekoppelte Geräte)
```

Ohne Docker: `go build -o housephone-bridge ./cmd/housephone-bridge`, dann `HOUSEPHONE_SIP_PASSWORD=... ./housephone-bridge -config config.yaml serve` (z. B. als systemd-Dienst).

### Fertiges Image und Kurzbefehl

- Das Image kommt aus der GitHub Container Registry, für amd64 und arm64:
  - `ghcr.io/jokeks2023/housephone-bridge:edge`: jeder Stand von `master`
  - `ghcr.io/jokeks2023/housephone-bridge:latest`: jede getaggte Version (`v…`)
- Selbst bauen: in `docker-compose.yml` `image:` auskommentieren und `build: .` aktivieren.
- Aktualisieren: `docker compose pull && docker compose up -d`.

`./housephone` spart das lange `docker compose exec …`:

```sh
./housephone pair -name "iPhone"   # Kopplungscode + QR-Code
./housephone devices list
./housephone identity              # Fingerabdruck der Bridge
./housephone tui                   # Admin-Oberfläche
```

### Admin-Oberfläche (`./housephone tui`)

Die TUI spricht nur über den Unix-Socket `/data/admin.sock` (Rechte 0600) mit der laufenden Bridge, es gibt keinen zusätzlichen Port. Sie passt in 80×24.

| Taste | Ansicht |
|---|---|
| `1` Übersicht | FRITZ!Box-Anmeldung, öffentliche IP, Push, TR-064, Geräte, Fingerabdruck; `k` zeigt die wirksame Konfiguration (Secrets geschwärzt) |
| `2` Geräte | `↑↓` auswählen, `r` umbenennen, `x` entfernen: die Verbindung wird **sofort** getrennt |
| `3` Kopplung | `n` erzeugt Code + QR-Code und wartet live, bis ein Gerät ihn benutzt; `Esc` widerruft |
| `4` Anrufe | aktive und letzte Anrufe (Nummern maskiert), Statistik seit Start und heute |
| `5` Logs | die letzten 1000 Zeilen live; `l` Level, `/` Suche |
| `6` Selbsttest | grün/gelb/rot je Prüfung, `↑↓` zeigt den Hinweis dazu, `r` prüft erneut |

`?` zeigt die Tastenhilfe, `q` beendet. Ohne Terminal (`./housephone tui | cat`) gibt der Befehl Übersicht und Selbsttest als Text aus. `devices remove` und `devices rename` gehen bei laufender Bridge ebenfalls über den Socket und wirken sofort.

**Admins für die App (ADR-0009):** `devices promote <id>` (TUI: Geräte → `a`) macht ein iPhone zum Admin; es verwaltet die Bridge dann in der App, nur im Heimnetz und mit Face ID, und richtet Face ID innerhalb einer Stunde ein. `devices demote <id>` (TUI: `A`) entzieht die Rechte sofort. Bei einer neuen Bridge ist das erste gekoppelte iPhone automatisch Admin. Jede Admin-Aktion wird allen Geräten gemeldet.

## 6. iPhone koppeln

```sh
docker compose exec housephone-bridge housephone-bridge pair -name "iPhone Joris"
```

Die Bridge zeigt einen QR-Code, einen Link, einen 16-stelligen Code (`XXXX-XXXX-XXXX-XXXX`) und ihren Fingerabdruck an. Gültig sind sie 10 Minuten und nur einmal. In der App erscheint die Kopplung beim ersten Start (und nach „Kopplung aufheben“): **QR-Code scannen** oder den Link einfügen.

Der Befehl wartet, bis ein Gerät den Code benutzt hat, und zeigt dann Name, Modell, Plattform und den Fingerabdruck seines Schlüssels. **Warst du das nicht, entferne das Gerät sofort** (`devices remove <id>`, steht in der Ausgabe). Strg-C bricht ab und macht den Code ungültig.

So funktioniert die Anmeldung (Details: `docs/architecture/ADR-0004-anmeldung-v2.md`):

- Jedes Gerät erzeugt beim Koppeln einen eigenen Schlüssel im Secure Enclave und beweist, dass es ihn besitzt. Die Bridge speichert nur den öffentlichen Schlüssel; es gibt kein Geheimnis, das kopiert werden könnte.
- Der QR-Code enthält den Fingerabdruck der Bridge. Die App koppelt nur mit der Bridge, deren Schlüssel dazu passt, und prüft danach jede Antwort. `housephone-bridge identity` zeigt ihn jederzeit, z. B. zum Vergleich mit dem `fp=` eines Kopplungslinks.
- Jede Anfrage ist signiert und nur einmal gültig; Uhrzeit von Gerät und Server dürfen höchstens 60 s abweichen. Antworten, Telefonbuch, Anrufliste und die ganze WebSocket-Verbindung sind zusätzlich Ende-zu-Ende verschlüsselt. Der Cloudflare Tunnel sieht nur verschlüsselte Daten.
- Koppelt sich ein neues Gerät, erfahren alle verbundenen Geräte davon.

## 7. Apple Watch

Für die Watch musst du auf dem Server und an der FRITZ!Box nichts einrichten:

- **Keine neue Portfreigabe:** Die Watch hat kein WebRTC. Ihr Ton läuft als A-law (8 kHz) in 20-ms-Rahmen über dieselbe WebSocket-Verbindung wie die Signalisierung, also durch den Cloudflare Tunnel. Die UDP-Freigabe 50000 braucht nur das iPhone.
- **Koppeln:** Die Watch wird über das gekoppelte iPhone gekoppelt (**Einstellungen → Apple Watch koppeln**). Das iPhone holt dafür einen frischen Code bei der Bridge und gibt ihn an die Uhr weiter. Die Uhr meldet sich danach selbst per HTTPS an (`POST /v1/pair`, `PUT /v1/device`).
  - Der Code gilt nur für eine Watch und nur, solange das iPhone gekoppelt ist. `devices list` zeigt in der Spalte „ÜBER“, über welches iPhone eine Watch gekoppelt wurde.
  - Die Uhr erzeugt dabei ihren eigenen Schlüssel; den Fingerabdruck der Bridge bekommt sie vom iPhone.
  - Entfernst du ein verlorenes iPhone, entfernt `devices remove` dessen Watches automatisch mit: Sie wurden über das iPhone gekoppelt und gelten deshalb als mitbetroffen.
  - watchOS erlaubt WebSocket nur während eines Anrufs. Die Uhr öffnet sie deshalb erst, wenn der VoIP-Push kommt.
- **Push:** Die Watch-App hat ein eigenes APNs-Topic (`com.jorisconrad.housephone.watchkitapp.voip`). Derselbe APNs-Key aus Schritt 3 gilt für alle Apps deines Teams.
  - Die Bridge akzeptiert nur Topics, die mit dem Bundle aus `apns.topic` beginnen (hier `com.jorisconrad.housephone.`) und auf `.voip` enden.
- **Codec:** Nimmst du an der Watch ab, beantwortet die Bridge den Anruf der FRITZ!Box mit PCMA. Am iPhone nimmt sie G.722 (HD). Umgewandelt wird nie.
  - Bietet die FRITZ!Box für einen Anruf kein PCMA an (sehr unüblich), klingelt die Watch für diesen Anruf nicht.

## 8. Telefonbuch und Anrufliste der FRITZ!Box (optional)

Mit diesem Schritt zeigen iPhone und Watch das FRITZ!Box-Telefonbuch (mit Favoriten) und die Anrufliste des Anschlusses, einschließlich der Anrufe am Schnurlostelefon und auf dem Anrufbeantworter. Eingehende Anrufe bekommen den Namen aus dem Telefonbuch. Die Bridge liest nur, sie ändert in der FRITZ!Box nichts.

1. **Benutzer anlegen:** `http://fritz.box` → **System → FRITZ!Box-Benutzer → Benutzer hinzufügen**.
   - Name z. B. `housephone`, ein langes Kennwort.
   - Als Recht **nur** „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“ ankreuzen.
2. **TR-064 erlauben:** **Heimnetz → Netzwerk → Netzwerkeinstellungen → „Zugriff für Anwendungen zulassen“** einschalten (Standard: an).
3. **Konfigurieren:**
   - `config.yaml`: `fritzbox.username: "housephone"`. `host` bleibt leer, dann gilt `sip.registrar`.
   - Kennwort:
     ```sh
     printf '%s' 'FRITZBOX-KENNWORT' > secrets/fritzbox_password && chmod 600 secrets/fritzbox_password
     ```
   - In `docker-compose.yml` ist `HOUSEPHONE_FRITZBOX_PASSWORD_FILE` schon eingetragen.
4. **Neu starten:** `docker compose up -d`. Im Log erscheinen `FRITZ!Box phonebook loaded` und `FRITZ!Box call list loaded`.

| Schlüssel in `config.yaml` | Standard | Bedeutung |
|---|---|---|
| `fritzbox.username` | leer (= aus) | FRITZ!Box-Benutzer; gesetzt = Funktion an |
| `fritzbox.host` | `sip.registrar` | FRITZ!Box, z. B. `192.168.0.1` |
| `fritzbox.port` | `49000` | TR-064-Port ohne TLS; nur für die Frage nach dem TLS-Port |
| `fritzbox.timezone` | `Europe/Berlin` | Zeitzone der Anrufliste |
| `fritzbox.countryCode` | `49` | Ländervorwahl ohne `+`, für den Namensabgleich |

Das Kennwort nie in `config.yaml` eintragen, sondern per `HOUSEPHONE_FRITZBOX_PASSWORD` oder, empfohlen, per `HOUSEPHONE_FRITZBOX_PASSWORD_FILE` (Datei `secrets/fritzbox_password`).

Technik:
- **Schnittstelle:** TR-064 (`X_AVM-DE_OnTel`) über HTTPS auf dem TLS-Port der FRITZ!Box (meist 49443), mit Digest-Anmeldung.
- **Zertifikat:** Die FRITZ!Box hat ein selbstsigniertes Zertifikat, das nicht geprüft wird. Die Verbindung ist trotzdem verschlüsselt.
- **Zwischenspeicher:** Telefonbuch 10 Minuten, Anrufliste 30 Sekunden.
- **Endpunkte:** `GET /v1/phonebook` und `GET /v1/history` (siehe `docs/protocol/signaling-v1.md`, v1.2).

## Betrieb

| Aufgabe | Befehl |
|---|---|
| Geräte anzeigen | `housephone-bridge devices list` (Spalte SCHLÜSSEL: Fingerabdruck des Geräteschlüssels) |
| Fingerabdruck der Bridge | `housephone-bridge identity` – derselbe Wert wie `fp=` im Kopplungslink |
| Gerät entfernen | `housephone-bridge devices remove <id>` – entfernt auch die Watches, die über dieses iPhone gekoppelt wurden (`-keep-companions` behält sie). Verbundene Geräte trennt die laufende Bridge innerhalb von 10 s. |
| Version | `housephone-bridge version` |
| Mehr Logs | `log.level: debug` bzw. `HOUSEPHONE_LOG_LEVEL=debug`. Auf Debug-Stufe kann die SIP-Bibliothek Details der SIP-Nachrichten mitschreiben, also auch Nummern. |
| Nummern im Log | Standardmäßig maskiert: nur die letzten 3 Ziffern (`…567`); von Anrufernamen nur, ob einer da ist (`hasCallerName`). `log.showNumbers: true` schreibt beides im Klartext, nur kurz zur Fehlersuche. |
| HD-Fehlersuche | Jeder eingehende Anruf loggt `incoming INVITE … offered=[…] chosen=…`. Fehlt `G722` in `offered`, bietet die FRITZ!Box für diesen Anruf kein HD an (z. B. oft bei Anrufen aus dem Mobilfunk). |

Daten liegen in `data/`:
- `bridge.json`: Bridge-ID
- `identity.key`: privater Schlüssel der Bridge (Ed25519, Modus 0600). Die Geräte kennen seinen Fingerabdruck und reden nur mit dieser Bridge.
- `devices.json`: Geräte mit ihren öffentlichen Schlüsseln und Push-Tokens (keine Geheimnisse)
- `pairing.json`: offene Kopplungscodes

Sichere diesen Ordner, vor allem `identity.key`. Verlierst du den Schlüssel, erzeugt die Bridge beim nächsten Start einen neuen. Die Geräte lehnen ihn ab, weil der Fingerabdruck nicht mehr passt; alle müssen neu gekoppelt werden. Die Sicherung ist so vertraulich wie ein Kennwort.

## Fehlersuche

| Symptom | Ursache / Lösung |
|---|---|
| `registration failed … 401/403` | Benutzername/Kennwort des IP-Telefons prüfen. Das IP-Telefon muss in der FRITZ!Box existieren. |
| `find local IP towards fritz.box` | `fritz.box` wird auf dem Server nicht aufgelöst → `sip.registrar` auf die IP der FRITZ!Box setzen (Standard `192.168.178.1`, bei manchen Anschlüssen z. B. `192.168.0.1`). |
| `sip.registrar: fritz.box löst auf … auf – das ist keine Adresse in deinem Heimnetz` | Dein Server fragt einen fremden DNS-Server (Pi-hole ohne Weiterleitung, 1.1.1.1 …). Dort gehört `fritz.box` einem Dritten; die Bridge würde ihm ihre Anmeldedaten schicken und startet deshalb nicht. → Die IP der FRITZ!Box eintragen. Die Bridge legt die Adresse beim Start fest und fragt DNS danach nicht mehr. |
| iPhone klingelt nicht, wenn die App geschlossen ist | APNs-Key/Key-ID prüfen. Log `push failed … 403 InvalidProviderToken` = Key/Team falsch. `BadDeviceToken` = Environment passt nicht (Debug vs. TestFlight); die App einmal öffnen, dann meldet sie das richtige Token. |
| Anruf wird angenommen, aber kein Ton (unterwegs) | UDP-Freigabe 50000 fehlt oder öffentliche IP falsch: Log `public IP` prüfen, ggf. `media.publicIp` setzen. |
| Kein Ton zu Hause | Server und iPhone müssen sich im LAN erreichen (kein Gast-WLAN, keine Client-Isolation). |
| `rejecting INVITE from unexpected source` | Die FRITZ!Box meldet sich von einer anderen IP als `sip.registrar` → dort die tatsächliche IP eintragen. |
| `no public IP configured` | STUN nicht erreichbar → `media.publicIp`/`publicHost` setzen. |
| `FRITZ!Box does not report its external IP via UPnP` | In der FRITZ!Box **Heimnetz → Netzwerk → Netzwerkeinstellungen → „Statusinformationen über UPnP übertragen“** einschalten. Bis dahin arbeitet die Bridge mit STUN. |
| Watch klingelt nicht | Log `push failed … DeviceTokenNotForTopic` = Topic der Watch passt nicht zum Token. Die Watch-App einmal öffnen, dann meldet sie Token und Topic neu. `not ringing websocket-pcma device` = der Anruf bot kein PCMA an. |
| `FRITZ!Box phonebook unavailable` / App zeigt „FRITZ!Box nicht erreichbar“ | „Zugriff für Anwendungen zulassen“ einschalten, `fritzbox.host` prüfen (Standard `sip.registrar`). |
| App zeigt „Anmeldung abgelehnt“ | Benutzer/Kennwort in `secrets/fritzbox_password` prüfen; der Benutzer braucht das Recht „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“. |
| Anrufliste fehlt, Telefonbuch geht | Die Anrufliste ist in der FRITZ!Box abgeschaltet (**Telefonie → Anrufe**) oder der Benutzer hat das Recht nicht. |
| Anrufe zeigen keinen Namen aus dem Telefonbuch | Die Nummer steht mehrfach mit verschiedenen Namen im Telefonbuch (dann bewusst kein Name), oder das Telefonbuch war beim Anruf noch nicht geladen (Log beim Start). |
| Watch-Gespräch stockt | Der Ton der Watch läuft über TCP (Tunnel). Die Bridge puffert höchstens 200 ms und verwirft ältere Rahmen. Bei schlechtem Netz lieber am iPhone annehmen. |

## Entwicklung

### Probetelefon (`housephone-probe`)

Ein minimales „Telefon“ für den Rechner. Damit lassen sich echte Anrufe über die echte FRITZ!Box testen, ohne iPhone, APNs oder Tunnel. Es verbindet sich wie die Watch (`websocket-pcma`, A-law über WebSocket) und meldet sich mit HP2 an. Sein Schlüssel liegt als Software-Schlüssel in `housephone-probe.json` (Modus 0600) – die Datei wie ein Kennwort behandeln.

```sh
go build -o housephone-probe ./cmd/housephone-probe
housephone-bridge pair -name "Probe"                                   # Link bzw. Code + Fingerabdruck merken
./housephone-probe -link 'housephone://pair?v=2&…'                     # einmal koppeln (Link aus pair)
./housephone-probe -url ws://127.0.0.1:8080/v1/ws -pair <CODE> -fp <FINGERABDRUCK>   # oder so
./housephone-probe                                                     # auf Anrufe warten, Echo
./housephone-probe -mode tone -record anruf.wav                        # 425-Hz-Ton senden, Empfang aufnehmen
./housephone-probe -dial 0170123456                                    # selbst anrufen
```

- **Echo:** Der Anrufer hört sich selbst. Damit ist der Ton in beide Richtungen belegt.
- **Pegel:** Einmal pro Sekunde erscheint der Pegel des empfangenen Tons.


```sh
go test -race ./...                              # alles außer dem Early-Media-Test
go test -run EarlyMedia ./internal/sipleg/       # ohne -race (Upstream-Race in diago)
E2E_LOG=1 go test -run EndToEnd -v ./internal/app/
```

| Paket | Aufgabe |
|---|---|
| `internal/app` | Verdrahtung + End-to-End-Tests (Fake-FRITZ!Box + Test-Gerät mit echtem WebRTC) |
| `internal/calls` | Anruf-Logik als Actor pro Anruf (eingehend, ausgehend, Multi-Device, Re-Attach) |
| `internal/sipleg` | FRITZ!Box-Seite (diago/sipgo): Registrierung, INVITE, CANCEL, DTMF (RFC 4733) |
| `internal/media` | WebRTC (pion) mit einem UDP-Port, öffentliche IP |
| `internal/signaling` | WebSocket-Server, Kopplung, Anmeldung (HP2) |
| `internal/hp2` | Anmeldung v2: Kopplungsnachweis, Signaturen, Sitzungsschlüssel, Verschlüsselung (ADR-0004); Testvektoren in `docs/protocol/fixtures/crypto` |
| `internal/push` | APNs-VoIP-Push |
| `internal/fritzbox` | TR-064: Telefonbuch, Anrufliste, Zwischenspeicher, Namen für eingehende Anrufe; `fritzboxtest` ist die Test-FRITZ!Box |
| `internal/store` | Dateibasierter Speicher (Geräte, Kopplungscodes, Bridge-Schlüssel) |

Abhängigkeiten: pion ist auf `webrtc v4.2.19` / `ice v4.4.0` festgelegt. Neuere Versionen nutzen `stun/v4`, das nicht zu diago v0.40.0 passt.
