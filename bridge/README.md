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

Die Bridge ermittelt ihre öffentliche IPv4 automatisch per STUN. Alternativ kannst du `media.publicIp` oder `media.publicHost` (z. B. deine MyFRITZ!-Adresse) setzen.

## 3. APNs-Key erstellen

1. [developer.apple.com](https://developer.apple.com/account/resources/authkeys/list) → **Certificates, Identifiers & Profiles → Keys → +**.
2. Namen vergeben und **Apple Push Notifications service (APNs)** ankreuzen, dann registrieren.
3. `AuthKey_XXXXXXXXXX.p8` herunterladen – **das geht nur einmal** – und die **Key ID** notieren.
4. Team-ID: `T9CA6D7T8N`, Topic: `com.jorisconrad.housephone.voip` (Standardwerte in `config.example.yaml`).

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

**Ohne Cloudflare:** Ein Reverse Proxy mit TLS (z. B. Caddy) auf Port 443 → `localhost:8080`, dazu eine TCP-Portfreigabe 443. In `config.yaml` dann `trustProxyHeaders` nur aktivieren, wenn der Proxy `X-Forwarded-For` setzt.

## 5. Starten (Docker Compose)

```sh
cd bridge
cp config.example.yaml config.yaml          # registrar, username, publicUrl anpassen
cp docker-compose.example.yml docker-compose.yml
mkdir -p data secrets
printf '%s' 'SIP-KENNWORT' > secrets/sip_password
cp ~/Downloads/AuthKey_XXXXXXXXXX.p8 secrets/apns_key.p8
chmod 600 secrets/*
# In docker-compose.yml: HOUSEPHONE_APNS_KEY_ID und user: "$(id -u):$(id -g)" eintragen
CLOUDFLARE_TUNNEL_TOKEN=... docker compose up -d
docker compose logs -f housephone-bridge    # erwartet: "registered at FRITZ!Box"
curl -s http://localhost:8080/v1/health     # {"sipRegistered":true,...}
```

Ohne Docker: `go build -o housephone-bridge ./cmd/housephone-bridge`, dann `HOUSEPHONE_SIP_PASSWORD=... ./housephone-bridge -config config.yaml serve` (z. B. als systemd-Dienst).

## 6. iPhone koppeln

```sh
docker compose exec housephone-bridge housephone-bridge pair -name "iPhone Joris"
```

Die Bridge zeigt einen QR-Code, einen Link und einen 10-stelligen Code an. Gültig sind sie 10 Minuten und nur einmal. In der App: **Einstellungen → Bridge koppeln** → QR-Code scannen.

## 7. Apple Watch

Für die Watch musst du auf dem Server und an der FRITZ!Box nichts einrichten:

- **Keine neue Portfreigabe:** Die Watch hat kein WebRTC. Ihr Ton läuft als A-law (8 kHz) in 20-ms-Rahmen über dieselbe WebSocket-Verbindung wie die Signalisierung, also durch den Cloudflare Tunnel. Die UDP-Freigabe 50000 braucht nur das iPhone.
- **Koppeln:** Die Watch wird über das gekoppelte iPhone gekoppelt (**Einstellungen → Apple Watch koppeln**). Das iPhone holt dafür einen frischen Code bei der Bridge und gibt ihn an die Uhr weiter. Die Uhr meldet sich danach selbst per HTTPS an (`POST /v1/pair`, `PUT /v1/device`).
  - watchOS erlaubt WebSocket nur während eines Anrufs. Die Uhr öffnet sie deshalb erst, wenn der VoIP-Push kommt.
- **Push:** Die Watch-App hat ein eigenes APNs-Topic (`com.jorisconrad.housephone.watchkitapp.voip`). Derselbe APNs-Key aus Schritt 3 gilt für alle Apps deines Teams.
  - Die Bridge akzeptiert nur Topics, die mit dem Bundle aus `apns.topic` beginnen (hier `com.jorisconrad.housephone.`) und auf `.voip` enden.
- **Codec:** Nimmst du an der Watch ab, beantwortet die Bridge den Anruf der FRITZ!Box mit PCMA. Am iPhone nimmt sie G.722 (HD). Umgewandelt wird nie.
  - Bietet die FRITZ!Box für einen Anruf kein PCMA an (sehr unüblich), klingelt die Watch für diesen Anruf nicht.

## Betrieb

| Aufgabe | Befehl |
|---|---|
| Geräte anzeigen | `housephone-bridge devices list` |
| Gerät entfernen | `housephone-bridge devices remove <id>` |
| Version | `housephone-bridge version` |
| Mehr Logs | `log.level: debug` bzw. `HOUSEPHONE_LOG_LEVEL=debug` |

Daten liegen in `data/`:
- `bridge.json`: Bridge-ID
- `devices.json`: Geräte mit gehashten Geheimnissen und Push-Tokens
- `pairing.json`: offene Kopplungscodes

Sichere diesen Ordner. Verlierst du ihn, müssen alle Geräte neu gekoppelt werden.

## Fehlersuche

| Symptom | Ursache / Lösung |
|---|---|
| `registration failed … 401/403` | Benutzername/Kennwort des IP-Telefons prüfen. Das IP-Telefon muss in der FRITZ!Box existieren. |
| `find local IP towards fritz.box` | `fritz.box` wird auf dem Server nicht aufgelöst → `sip.registrar` auf die IP der FRITZ!Box setzen (Standard `192.168.178.1`, bei manchen Anschlüssen z. B. `192.168.0.1`). |
| iPhone klingelt nicht, wenn die App geschlossen ist | APNs-Key/Key-ID prüfen. Log `push failed … 403 InvalidProviderToken` = Key/Team falsch. `BadDeviceToken` = Environment passt nicht (Debug vs. TestFlight); die App einmal öffnen, dann meldet sie das richtige Token. |
| Anruf wird angenommen, aber kein Ton (unterwegs) | UDP-Freigabe 50000 fehlt oder öffentliche IP falsch: Log `public IP` prüfen, ggf. `media.publicIp` setzen. |
| Kein Ton zu Hause | Server und iPhone müssen sich im LAN erreichen (kein Gast-WLAN, keine Client-Isolation). |
| `rejecting INVITE from unexpected source` | Die FRITZ!Box meldet sich von einer anderen IP als `sip.registrar` → dort die tatsächliche IP eintragen. |
| `no public IP configured` | STUN nicht erreichbar → `media.publicIp`/`publicHost` setzen. |
| Watch klingelt nicht | Log `push failed … DeviceTokenNotForTopic` = Topic der Watch passt nicht zum Token. Die Watch-App einmal öffnen, dann meldet sie Token und Topic neu. `not ringing websocket-pcma device` = der Anruf bot kein PCMA an. |
| Watch-Gespräch stockt | Der Ton der Watch läuft über TCP (Tunnel). Die Bridge puffert höchstens 200 ms und verwirft ältere Rahmen. Bei schlechtem Netz lieber am iPhone annehmen. |

## Entwicklung

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
| `internal/signaling` | WebSocket-Server, Kopplung, Auth |
| `internal/push` | APNs-VoIP-Push |
| `internal/store` | Dateibasierter Speicher (Geräte, Kopplungscodes) |

Abhängigkeiten: pion ist auf `webrtc v4.2.19` / `ice v4.4.0` festgelegt. Neuere Versionen nutzen `stun/v4`, das nicht zu diago v0.40.0 passt.
