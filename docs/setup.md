# Housephone einrichten – bis zum ersten Anruf

Diese Schritte gehen nur mit dir, weil sie deinen Server, deine FRITZ!Box, dein iPhone und deine Accounts brauchen.
Einmal durchgearbeitet, klingelt dein iPhone bei Festnetzanrufen – zu Hause und unterwegs.

| Wo | Was | Dauer |
|---|---|---|
| FRITZ!Box | IP-Telefon „Housephone“ anlegen, UDP 50000 freigeben | 5 min |
| developer.apple.com | APNs-Key erzeugen | 3 min |
| Cloudflare | Tunnel für `phone.<deine-domain>` anlegen | 5 min |
| Server | Bridge per Docker starten | 10 min |
| Mac + iPhone | App bauen, installieren und koppeln | 10 min |

## 1. Bridge einrichten

Folge [`bridge/README.md`](../bridge/README.md), Schritte 1–5. Deine Werte:

- **FRITZ!Box-Adresse:** `192.168.0.1`.
  - `fritz.box` funktioniert, wenn dein Server die FRITZ!Box als DNS nutzt.
  - Sonst `sip.registrar: "192.168.0.1"` in `config.yaml` eintragen.
- **Team-ID:** `T9CA6D7T8N`, **Topic:** `com.jorisconrad.housephone.voip`. Beides steht schon so in `config.example.yaml`.
- **Anschluss:** Vodafone Red Business hat eine öffentliche IPv4. Ist sie fest, trag sie als `media.publicIp` ein – das ist robuster als die automatische Erkennung.

Fertig ist dieser Schritt, wenn `curl -s http://localhost:8080/v1/health` auf dem Server `"sipRegistered":true` meldet und die FRITZ!Box unter **Telefonie → Telefoniegeräte** das IP-Telefon „Housephone“ als verbunden zeigt.

## 2. App aufs iPhone

```sh
cd ios
xcodegen generate
open Housephone.xcodeproj
```

1. In Xcode oben dein iPhone als Ziel wählen, Scheme **Housephone**, dann ▶︎.
   - Der Build ist klein, weil WebRTC fertig kompiliert kommt; das geht auch auf dem M1 mit 8 GB.
   - Schließ vorher andere große Apps.
2. **Beim ersten Mal:**
   - auf dem iPhone **Einstellungen → Datenschutz & Sicherheit → Entwicklermodus** einschalten,
   - in Xcode unter *Signing & Capabilities* prüfen, dass Team `T9CA6D7T8N` gesetzt ist.
3. **Falls Xcode „Push Notifications“ bemängelt:** [developer.apple.com → Identifiers](https://developer.apple.com/account/resources/identifiers/list) → `com.jorisconrad.housephone` → *Push Notifications* aktivieren und erneut bauen.

Aus Xcode installierte Builds nutzen die APNs-Sandbox. Die App meldet das der Bridge selbst, du musst nichts einstellen.

## 3. Koppeln

Auf dem Server:

```sh
docker compose exec housephone-bridge housephone-bridge pair -name "iPhone"
```

In der App öffnet sich beim ersten Start die Kopplung:

1. **QR-Code scannen.** Alternativ den Link aus dem Terminal kopieren und in der App einfügen.
2. **Mikrofon erlauben.** Kontakte sind optional; ohne sie siehst du nur Nummern.

Unter **Einstellungen** steht danach **Verbunden** (grün). Steht dort „FRITZ!Box nicht angemeldet“, stimmt Schritt 1 noch nicht.

## 4. Ausprobieren

- [ ] **Ausgehend (WLAN):** Nummer im Tastenfeld wählen → Freiton → Gespräch → Auflegen.
- [ ] **Eingehend, App offen:** Festnetz von einem anderen Telefon anrufen → CallKit klingelt → Annehmen → Ton in beide Richtungen.
- [ ] **Eingehend, gesperrt, unterwegs:** WLAN am iPhone **aus**, App im App-Umschalter schließen, iPhone sperren, Festnetz anrufen → klingelt wie ein normaler Anruf → Annehmen → Ton.
- [ ] **Rückruf aus der iPhone-Telefon-App:** Den Anruf in *Telefon → Anrufliste* antippen → startet über Housephone.
- [ ] **Anderes Telefon nimmt an:** Festnetz anrufen und am Schnurlostelefon abheben → iPhone hört auf zu klingeln („an anderem Gerät angenommen“).

## 5. Wenn etwas nicht klappt

- **Server-Seite:** siehe Tabelle *Fehlersuche* in [`bridge/README.md`](../bridge/README.md).
  - `docker compose logs -f housephone-bridge` zeigt Registrierung, Pushes, Anrufe und ICE.
  - Mit `HOUSEPHONE_LOG_LEVEL=debug` wird es ausführlicher.
- **App-Seite:** Konsole.app auf dem Mac, iPhone auswählen, nach Subsystem `com.jorisconrad.housephone` filtern.
  - Kategorien: `calls` (CallKit/PushKit), `media` (WebRTC/ICE), `bridge` (Verbindung).
- **Typische Ursachen:**

| Symptom | Ursache |
|---|---|
| Klingelt nur bei offener App | APNs-Key, Key-ID oder Team falsch; Log `push failed` auf der Bridge |
| Klingelt, aber kein Ton unterwegs | UDP-Freigabe 50000 fehlt oder öffentliche IP falsch |
| „Bridge nicht erreichbar“ | Cloudflare Tunnel läuft nicht, oder `publicUrl` in `config.yaml` stimmt nicht mit dem Hostnamen überein |
