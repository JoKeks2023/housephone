# Housephone

**Mit iPhone und Apple Watch über deine FRITZ!Box telefonieren – zu Hause und unterwegs, ohne VPN.**
Anrufe klingeln wie normale Anrufe (CallKit), auch bei gesperrtem iPhone und direkt an der Watch.

```
                      Heimnetz                                    Unterwegs / WLAN
┌───────────────────────────────────────────────┐
│  FRITZ!Box ◄── SIP (LAN) ──► Bridge (Docker)  │ ◄── Steuerung: Cloudflare Tunnel ──► iPhone · Apple Watch
│                                   │           │ ◄══ Ton: UDP 50000 (Portfreigabe) ══►
│                                   └── Push (Apple APNs) ──────────────────────────► weckt iPhone · Watch
└───────────────────────────────────────────────┘
```

- **Bridge:** ein kleiner Dienst auf deinem Heimserver. Sie meldet sich an der FRITZ!Box als IP-Telefon an, weckt deine Geräte per Push und reicht Gespräche durch.
- **iPhone-App:** Tastenfeld, Anrufliste (auch die der FRITZ!Box), Kontakte (iPhone und FRITZ!Box-Telefonbuch).
- **Watch-App:** klingelt selbst. Du nimmst am Handgelenk an, auch ohne iPhone in der Nähe.
- **Sicherheit:** Die Geräteschlüssel liegen im Secure Enclave, und die Verbindung zwischen Gerät und Bridge ist Ende-zu-Ende verschlüsselt, also auch gegenüber Cloudflare. Kopplungscodes gibt es nur auf dem Server.

---

## Inhalt

1. [Was du brauchst](#was-du-brauchst)
2. [FRITZ!Box vorbereiten](#1-fritzbox-vorbereiten)
3. [Apple: Push-Schlüssel (APNs)](#2-apple-push-schlüssel-apns)
4. [Cloudflare Tunnel](#3-cloudflare-tunnel)
5. [Bridge auf dem Server starten](#4-bridge-auf-dem-server-starten)
6. [Selbsttest in der TUI](#5-selbsttest-in-der-tui)
7. [App aufs iPhone (und die Watch)](#6-app-aufs-iphone-und-die-watch)
8. [iPhone koppeln](#7-iphone-koppeln)
9. [Optional: Telefonbuch und Anrufliste der FRITZ!Box](#8-optional-telefonbuch-und-anrufliste)
10. [Ausprobieren](#9-ausprobieren)
11. [Alltag: Befehle, Updates, Backup](#alltag-befehle-updates-backup)
12. [Wenn etwas nicht klappt](#wenn-etwas-nicht-klappt)
13. [Wie sicher ist das?](#wie-sicher-ist-das)
14. [Entwicklung](#entwicklung)

---

## Was du brauchst

| | Wofür | Dauer |
|---|---|---|
| **FRITZ!Box** mit Telefonie | Die Bridge meldet sich dort als IP-Telefon an | 10 min |
| **Server im Heimnetz**, der immer läuft (Linux mit Docker, z. B. Mini-PC, NAS, Raspberry Pi 4/5) | Läuft die Bridge | 10 min |
| **Apple-Developer-Account** (bezahlt) | Push-Schlüssel, damit das iPhone klingelt, wenn die App zu ist | 5 min |
| **Cloudflare-Account** mit eigener Domain (kostenlos) | Damit iPhone und Watch unterwegs die Bridge erreichen, ohne VPN | 10 min |
| **Mac mit Xcode** und dein iPhone | App installieren | 15 min |

> 💡 Es wird **kein** TCP-Port geöffnet. Freigegeben wird nur **ein** UDP-Port für den Ton.

---

## 1. FRITZ!Box vorbereiten

Öffne `http://fritz.box` bzw. die IP deiner Box, bei Vodafone-Kabel z. B. `http://192.168.0.1`.

### 1a · Dem Server eine feste IP geben

**Heimnetz → Netzwerk → Netzwerkverbindungen** → beim Server auf ✏️ → **„Diesem Netzwerkgerät immer die gleiche IPv4-Adresse zuweisen“** → Übernehmen.
Notiere dir die IP, z. B. `192.168.0.50`.

### 1b · IP-Telefon „Housephone“ anlegen

1. **Telefonie → Telefoniegeräte → Neues Gerät einrichten**
2. **Telefon (mit und ohne Anrufbeantworter)** → **LAN/WLAN (IP-Telefon)** → Name `Housephone` → Weiter
3. **Benutzername** (z. B. `housephone`) und ein **langes Kennwort** vergeben und notieren
4. Wähle, mit welcher Nummer das Gerät **rausruft** und auf welche Nummern es **reagiert**
5. **„Anmeldung aus dem Internet erlauben“ bleibt aus.** Die Bridge steht im Heimnetz.

### 1c · Portfreigabe für den Ton (UDP 50000)

**Internet → Freigaben → Portfreigaben → Gerät für Freigaben hinzufügen** → deinen Server wählen → **Neue Freigabe** → **Portfreigabe**:

| Anwendung | Protokoll | Port an Gerät | bis Port | Port extern gewünscht |
|---|---|---|---|---|
| Andere Anwendung „Housephone Ton“ | **UDP** | 50000 | 50000 | 50000 |

Mehr muss nicht offen sein. SIP bleibt im Heimnetz, die Steuerung läuft über den Tunnel.

✅ **Geschafft, wenn:** die Freigabe in der Liste aktiv ist (grüner Punkt).

---

## 2. Apple: Push-Schlüssel (APNs)

Damit das iPhone klingelt, obwohl die App geschlossen ist, schickt die Bridge einen VoIP-Push über Apple.

1. [developer.apple.com → Certificates, Identifiers & Profiles → **Keys**](https://developer.apple.com/account/resources/authkeys/list) → **＋**
2. Name z. B. `Housephone Push`, **Apple Push Notifications service (APNs)** ankreuzen → Continue → Register
3. **`AuthKey_XXXXXXXXXX.p8` herunterladen.** Das geht **nur einmal**, also gut aufbewahren.
4. Die **Key ID** notieren (10 Zeichen, z. B. `ABCDE12345`)

Team-ID (`T9CA6D7T8N`) und Topic (`com.jorisconrad.housephone.voip`) sind in der Beispielkonfiguration bereits eingetragen.

---

## 3. Cloudflare Tunnel

Über den Tunnel erreichen iPhone und Watch die Bridge von unterwegs, ohne offenen TCP-Port und ohne VPN.

1. [one.dash.cloudflare.com](https://one.dash.cloudflare.com) → **Networks → Tunnels → Create a tunnel** → **Cloudflared** → Name `housephone` → Save
2. Bei „Install and run a connector“ **nichts installieren**, nur den **Token** kopieren (die lange Zeichenkette nach `--token`)
3. **Public Hostname** hinzufügen:
   - Subdomain `phone`, Domain `deine-domain.de`
   - Service Type **HTTP**, URL **`localhost:8080`**
   - Save

Deine Adresse für die Bridge lautet dann **`wss://phone.deine-domain.de/v1/ws`**.

> Der Tunnel trägt nur die Steuerung (WebSocket). Der Ton läuft über UDP 50000, weil Cloudflare Tunnel kein UDP weiterleitet.

---

## 4. Bridge auf dem Server starten

Voraussetzung: Docker mit Compose-Plugin ([Anleitung](https://docs.docker.com/engine/install/)).

```sh
git clone https://github.com/JoKeks2023/housephone.git
cd housephone/bridge

cp config.example.yaml config.yaml
cp docker-compose.example.yml docker-compose.yml
mkdir -p data secrets
```

### 4a · `config.yaml` anpassen

Nur diese Zeilen musst du ändern:

```yaml
bridge:
  name: "Zuhause"                                   # Name in der App
  publicUrl: "wss://phone.deine-domain.de/v1/ws"    # aus Schritt 3
sip:
  registrar: "192.168.0.1"                          # IP deiner FRITZ!Box, keine Namen wie fritz.box
  username: "housephone"                            # Benutzername aus Schritt 1b
```

> ⚠️ Trag bei `registrar` die **IP** der FRITZ!Box ein. Die Bridge startet absichtlich nicht, wenn die Adresse nicht im Heimnetz liegt. `fritz.box` kann über öffentliches DNS zu einem fremden Server auflösen.

### 4b · Geheimnisse ablegen

```sh
printf '%s' 'KENNWORT-DES-IP-TELEFONS' > secrets/sip_password
cp ~/Downloads/AuthKey_XXXXXXXXXX.p8 secrets/apns_key.p8
echo 'CLOUDFLARE_TUNNEL_TOKEN=DEIN-TOKEN' > .env
chmod 600 secrets/* .env
```

In `docker-compose.yml` die **Key ID** eintragen:

```yaml
      HOUSEPHONE_APNS_KEY_ID: "ABCDE12345"
```

Der Container läuft als Benutzer `1000:1000`. Falls dein Benutzer eine andere ID hat, gilt eine der beiden Varianten:

```sh
sudo chown -R 1000:1000 data secrets   # entweder so
# oder in docker-compose.yml:  user: "$(id -u):$(id -g)" durch deine Werte ersetzen
```

### 4c · Starten

```sh
docker compose up -d
docker compose logs -f housephone-bridge
```

✅ **Geschafft, wenn** im Log steht: `registered at FRITZ!Box`. In der FRITZ!Box erscheint „Housephone“ unter **Telefonie → Telefoniegeräte** als verbunden.

Das Image kommt fertig aus der GitHub Container Registry, für amd64 und arm64. Selbst bauen: in `docker-compose.yml` `image:` auskommentieren und `build: .` aktivieren.

---

## 5. Selbsttest in der TUI

```sh
./housephone tui
```

Mit **6** öffnest du den **Selbsttest**. Jede Zeile ist grün, gelb oder rot. Mit ↑/↓ siehst du bei jedem Punkt, was zu tun ist.

```
  ● FRITZ!Box-Anmeldung      angemeldet an 192.168.0.1
  ● Push (APNs)              eingerichtet
  ● Öffentliche IP           94.x.x.x (von der FRITZ!Box)
  ● Medienport               UDP 50000 lokal offen   ← die Freigabe in der FRITZ!Box musst du selbst prüfen
  ● Öffentliche Adresse      wss://phone.deine-domain.de/v1/ws
```

Weitere Tabs: **1** Übersicht · **2** Geräte · **3** Kopplung · **4** Anrufe · **5** Logs. **?** zeigt die Hilfe, **q** beendet.

---

## 6. App aufs iPhone (und die Watch)

Auf dem Mac, einmalig:

```sh
brew install xcodegen
cd housephone/ios
xcodegen generate
open Housephone.xcodeproj
```

1. In Xcode oben dein **iPhone** als Ziel wählen, Scheme **Housephone** → ▶︎
2. **Beim ersten Mal:**
   - auf dem iPhone **Einstellungen → Datenschutz & Sicherheit → Entwicklermodus** einschalten
   - in Xcode unter *Signing & Capabilities* prüfen, dass dein Team gesetzt ist
3. **Watch-App:** Die Watch-App steckt in der iPhone-App. Auf dem iPhone in der **Watch-App → Verfügbare Apps → Housephone → Installieren**.

> Falls Xcode „Push Notifications“ bemängelt: auf [developer.apple.com → Identifiers](https://developer.apple.com/account/resources/identifiers/list) bei `com.jorisconrad.housephone` bzw. `…watchkitapp` **Push Notifications** aktivieren.

---

## 7. iPhone koppeln

Auf dem Server:

```sh
./housephone pair -name "iPhone Joris"
```

Es erscheinen ein **QR-Code** und ein Code wie `K7P2-XH9Q-RMW4-DZT8`. Er ist 10 Minuten gültig und einmalig.

1. Öffne die App. Beim ersten Start erscheint die Kopplung → **QR-Code scannen**, oder den Link kopieren und einsetzen.
2. Die App prüft, dass die Bridge wirklich deine ist (Fingerabdruck im QR-Code).
3. **Mikrofon erlauben.** Kontakte sind optional.
4. Im Terminal meldet `pair`, welches Gerät gekoppelt wurde. Warst du es nicht, entfernst du es sofort mit `./housephone devices remove <ID>`.

**Apple Watch:** Du musst nichts tun. Sobald das iPhone verbunden und die Watch-App installiert ist, koppelt sich die Watch automatisch mit eigenem Schlüssel.

✅ **Geschafft, wenn** in der App unter **Einstellungen** „Verbunden“ steht und die Watch „Bereit für Anrufe“ zeigt.

---

## 8. Optional: Telefonbuch und Anrufliste

Damit zeigen App und Watch das FRITZ!Box-Telefonbuch, die Anrufliste des ganzen Anschlusses und Namen bei eingehenden Anrufen.

1. **FRITZ!Box:** **System → FRITZ!Box-Benutzer → Benutzer hinzufügen**. Name `housephone`, ein Kennwort, Recht **„Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“**. Keine weiteren Rechte.
2. **FRITZ!Box:** **Heimnetz → Netzwerk → Netzwerkeinstellungen → „Zugriff für Anwendungen zulassen“** muss an sein (Standard).
3. **Server:**
   ```sh
   printf '%s' 'KENNWORT' > secrets/fritzbox_password && chmod 600 secrets/fritzbox_password
   ```
   In `config.yaml`:
   ```yaml
   fritzbox:
     username: "housephone"
   ```
4. `docker compose up -d`. In der TUI unter Übersicht steht dann „TR-064 ● eingerichtet“.

---

## 9. Ausprobieren

- [ ] **Ausgehend:** Nummer im Tastenfeld wählen → Freiton → Gespräch
- [ ] **Eingehend, unterwegs:** WLAN am iPhone aus, App schließen, iPhone sperren, Festnetz vom Handy anrufen → klingelt wie ein normaler Anruf → Ton in beide Richtungen
- [ ] **Watch:** Festnetz anrufen → Watch klingelt selbst → an der Watch annehmen
- [ ] **Anderes Telefon nimmt ab** → iPhone und Watch hören auf zu klingeln
- [ ] **Rückruf aus der iPhone-Anrufliste** startet Housephone

---

## Alltag: Befehle, Updates, Backup

Alle Befehle im Ordner `housephone/bridge`:

| Aufgabe | Befehl |
|---|---|
| Admin-Oberfläche | `./housephone tui` |
| Neues Gerät koppeln | `./housephone pair -name "iPad"` |
| Geräte anzeigen | `./housephone devices list` |
| Gerät entfernen (wirkt sofort) | `./housephone devices remove <ID>` |
| Fingerabdruck der Bridge | `./housephone identity` |
| Logs | `docker compose logs -f housephone-bridge` |
| **Update** | `docker compose pull && docker compose up -d` |

**Backup:** Sichere den Ordner `data/`, vor allem `identity.key` und `devices.json`. Geht `identity.key` verloren, müssen alle Geräte neu gekoppelt werden.

**Mehr Details:** Die Konfiguration steht im Kommentar von `config.example.yaml`, der Betrieb in [`bridge/README.md`](bridge/README.md).

---

## Wenn etwas nicht klappt

| Symptom | Lösung |
|---|---|
| Bridge startet nicht: „… nicht im Heimnetz“ | `sip.registrar` auf die **IP** der FRITZ!Box setzen |
| `registration failed … 401/403` | Benutzername/Kennwort des IP-Telefons prüfen (Schritt 1b) |
| iPhone klingelt nur bei offener App | APNs-Key, Key-ID bzw. Team prüfen. Im Log steht dann `push failed`. |
| Klingelt, aber kein Ton unterwegs | UDP-Portfreigabe 50000 fehlt (Schritt 1c) |
| Kein Ton zu Hause | Server und iPhone im selben Netz? Kein Gast-WLAN. |
| App: „Bridge nicht erreichbar“ | Läuft der Tunnel (`docker compose ps`)? Stimmt `publicUrl` mit dem Hostnamen aus Schritt 3 überein? |
| App: „Bridge nicht vertrauenswürdig“ | Die Bridge hat einen anderen Schlüssel als beim Koppeln, z. B. weil `data/` gelöscht wurde. Neu koppeln. |
| App: „Uhrzeit prüfen“ | Die Uhr des iPhones oder Servers geht falsch. Automatische Uhrzeit einschalten. |
| Telefonbuch fehlt | Schritt 8. Hat der FRITZ!Box-Benutzer das Recht „…Anrufliste“? |

Mehr Details liefern `./housephone tui` → **5 Logs**; auf dem iPhone Konsole.app mit Subsystem `com.jorisconrad.housephone`. Telefonnummern stehen im Log standardmäßig nur gekürzt (`…563`).

---

## Wie sicher ist das?

- **Geräteschlüssel:** Jedes Gerät hat einen eigenen Schlüssel im **Secure Enclave**. Er kann das Gerät nicht verlassen, auch nicht per Backup.
- **Beidseitige Prüfung:** Die App prüft bei jeder Verbindung, dass sie mit **deiner** Bridge spricht; der Fingerabdruck steckt im QR-Code.
- **Ende-zu-Ende-Verschlüsselung:** Steuerung, Telefonbuch, Anrufliste und der Ton der Watch sind Ende-zu-Ende verschlüsselt, auch gegenüber Cloudflare. Der Ton des iPhones läuft über WebRTC (DTLS-SRTP).
- **Kopplungscodes** gibt es nur auf dem Server: 80 Bit, einmalig, 10 Minuten gültig. Jede neue Kopplung wird allen Geräten angezeigt.
- **Container:** gehärtet, also schreibgeschützt, ohne Root und ohne Linux-Capabilities.
- **Details:** [ADR-0004](docs/architecture/ADR-0004-anmeldung-v2.md), [Sicherheits-Review](docs/reviews/).

---

## Entwicklung

| Thema | Dokument |
|---|---|
| Architektur | [ADR-0001 Bridge](docs/architecture/ADR-0001-bridge-architektur.md) · [ADR-0002 Watch](docs/architecture/ADR-0002-watch.md) · [ADR-0003 Telefonbuch](docs/architecture/ADR-0003-fritzbox-telefonbuch-anrufliste.md) · [ADR-0004 Anmeldung v2](docs/architecture/ADR-0004-anmeldung-v2.md) |
| Protokoll | [v1](docs/protocol/signaling-v1.md) · [v2](docs/protocol/signaling-v2.md) · [Fixtures](docs/protocol/fixtures/) |
| Bridge im Detail | [`bridge/README.md`](bridge/README.md) |
| Apps bauen | [`ios/README.md`](ios/README.md) |
| Zukunftsplan | [`docs/roadmap.md`](docs/roadmap.md) |
| Veröffentlichung | [`docs/veroeffentlichung.md`](docs/veroeffentlichung.md) |
| Aufgaben, Berichte, Reviews | [`docs/tasks/`](docs/tasks/) · [`docs/reviews/`](docs/reviews/) |

```sh
cd bridge && go test -race ./...                    # Bridge
cd ios/Packages/HousephoneKit && swift test         # Protokoll, Krypto, Zustände
```

Die CI testet die Bridge (inkl. gehärtetem Docker-Start), baut die iPhone-App samt Watch-App und veröffentlicht das Bridge-Image auf GHCR.

---

## Legacy: Telephone für macOS

Dieses Repository ist aus [64characters/Telephone](https://github.com/64characters/Telephone) entstanden. Der macOS-Code (`Telephone/`, `Domain/`, `UseCases/` …) liegt weiterhin hier und dient als Referenz. Die ursprüngliche Anleitung folgt unverändert.

Telephone is a VoIP SIP softphone for Mac. It allows you to make phone
calls over the Internet or your company network. If your phone line
supports SIP protocol, you can use it on your Mac instead of a
physical phone anywhere you have a decent network connection.

### Building (legacy)

#### Opus

Opus codec is optional.

Download:

    $ curl -O https://archive.mozilla.org/pub/opus/opus-1.3.1.tar.gz
    $ tar xzvf opus-1.3.1.tar.gz
    $ cd opus-1.3.1

Build and install:

    $ ./configure --prefix=/path/to/Telephone/ThirdParty/Opus --disable-shared CFLAGS='-arch arm64 -arch x86_64 -Os -mmacosx-version-min=15.6'
    $ make
    $ make install

#### LibreSSL

Download:

    $ curl -O https://ftp.openbsd.org/pub/OpenBSD/LibreSSL/libressl-3.1.5.tar.gz
    $ curl -O https://ftp.openbsd.org/pub/OpenBSD/LibreSSL/libressl-3.1.5.tar.gz.asc
    $ gpg --verify libressl-3.1.5.tar.gz.asc
    $ tar xzvf libressl-3.1.5.tar.gz
    $ cd libressl-3.1.5

Build and install:

    $ ./configure --prefix=/path/to/Telephone/ThirdParty/LibreSSL --disable-shared CFLAGS='-arch arm64 -arch x86_64 -Os -mmacosx-version-min=15.6'
    $ make
    $ make install

#### PJSIP

Download:

    $ curl -o pjproject-2.10.tar.gz https://codeload.github.com/pjsip/pjproject/tar.gz/2.10
    $ tar xzvf pjproject-2.10.tar.gz
    $ cd pjproject-2.10

Create `pjlib/include/pj/config_site.h`:

    #define PJSIP_DONT_SWITCH_TO_TCP 1
    #define PJSUA_MAX_ACC 32
    #define PJMEDIA_RTP_PT_TELEPHONE_EVENTS 101
    #define PJ_DNS_MAX_IP_IN_A_REC 32
    #define PJ_DNS_SRV_MAX_ADDR 32
    #define PJSIP_MAX_RESOLVED_ADDRESSES 32
    #define PJ_HAS_IPV6 1

Patch:

    $ patch -p0 -i /path/to/Telephone/ThirdParty/PJSIP/patches/sock_qos_darwin.patch
    $ patch -p0 -i /path/to/Telephone/ThirdParty/PJSIP/patches/os_core_unix.patch
    $ patch -p0 -i /path/to/Telephone/ThirdParty/PJSIP/patches/coreaudio_dev.patch

Build and install (remove `--with-opus` option if you don’t need Opus):

    $ ./configure --prefix=/path/to/Telephone/ThirdParty/PJSIP --with-opus=/path/to/Telephone/ThirdParty/Opus --with-ssl=/path/to/Telephone/ThirdParty/LibreSSL --disable-video --disable-libyuv --disable-libwebrtc --host=arm-apple-darwin CFLAGS='-arch arm64 -arch x86_64 -Os -DNDEBUG -mmacosx-version-min=15.6' CXXFLAGS='-arch arm64 -arch x86_64 -Os -DNDEBUG -mmacosx-version-min=15.6'
    $ make lib
    $ make install

    
Build Telephone.

### Contribution

For the legal reasons, pull requests are not accepted. Please feel
free to share your thoughts and ideas by commenting on the issues.
