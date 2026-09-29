# Housephone einrichten – bis zum ersten Anruf

Diese Schritte gehen nur mit dir, weil sie deinen Server, deine FRITZ!Box, dein iPhone und deine Accounts brauchen.
Einmal durchgearbeitet, klingeln dein iPhone und deine Apple Watch bei Festnetzanrufen – zu Hause und unterwegs.

| Wo | Was | Dauer |
|---|---|---|
| FRITZ!Box | IP-Telefon „Housephone“ anlegen, UDP 50000 freigeben | 5 min |
| developer.apple.com | APNs-Key erzeugen | 3 min |
| Cloudflare | Tunnel für `phone.<deine-domain>` anlegen | 5 min |
| Server | Bridge per Docker starten | 10 min |
| Mac + iPhone | App bauen, installieren und koppeln | 10 min |
| iPhone + Watch | Apple Watch koppeln | 2 min |
| FRITZ!Box + Server | Zugang für Telefonbuch und Anrufliste (optional) | 3 min |

## 1. Bridge einrichten

Folge [`bridge/README.md`](../bridge/README.md), Schritte 1–5. Deine Werte:

- **FRITZ!Box-Adresse:** `sip.registrar: "192.168.0.1"` in `config.yaml` eintragen, also die IP, nicht `fritz.box`.
  - Über fremde DNS-Server löst `fritz.box` zu einem öffentlichen Server auf. Die Bridge prüft das beim Start und startet nur mit einer Adresse im Heimnetz.
- **Team-ID:** `T9CA6D7T8N`, **Topic:** `com.jorisconrad.housephone.voip`. Beides steht schon so in `config.example.yaml`.
- **Anschluss:** Vodafone Red Business hat eine öffentliche IPv4, die sich ändern kann. Die Bridge fragt sie alle 30 s bei der FRITZ!Box ab (UPnP), du musst nichts eintragen.

Fertig ist dieser Schritt, wenn `docker compose logs housephone-bridge` die Zeile `registered at FRITZ!Box` zeigt und die FRITZ!Box unter **Telefonie → Telefoniegeräte** das IP-Telefon „Housephone“ als verbunden zeigt. (`curl -s http://localhost:8080/v1/health` sagt ohne Anmeldung nur `{"status":"ok"}`, also dass die Bridge läuft.)

Am schnellsten prüfst du das mit `./housephone tui` (im Ordner der `docker-compose.yml`): Taste `6` zeigt den **Selbsttest** mit grün/gelb/rot je Prüfung (FRITZ!Box-Anmeldung, Push, öffentliche IP, Medienport, öffentliche Adresse, Geräte) und mit `↑↓` den Hinweis, was zu tun ist. Taste `3` koppelt ein Gerät mit QR-Code, Taste `5` zeigt die Logs live.

Der Bridge-Schlüssel liegt danach in `data/identity.key`. Sichere ihn zusammen mit `data/` – geht er verloren, müssen alle Geräte neu gekoppelt werden.

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
3. **Falls Xcode „Push Notifications“ bemängelt:** [developer.apple.com → Identifiers](https://developer.apple.com/account/resources/identifiers/list) → `com.jorisconrad.housephone` → *Push Notifications* aktivieren und erneut bauen. Dasselbe gilt für die Watch-App `com.jorisconrad.housephone.watchkitapp`.

Die Watch-App steckt in der iPhone-App und wird mitgebaut. Auf die Uhr kommt sie automatisch, wenn in der Watch-App des iPhones *Apps automatisch installieren* aktiv ist. Sonst: *Watch-App → Verfügbare Apps → Housephone → Installieren*.

Aus Xcode installierte Builds nutzen die APNs-Sandbox. Die App meldet das der Bridge selbst, du musst nichts einstellen.

## 3. Koppeln

Auf dem Server:

```sh
docker compose exec housephone-bridge housephone-bridge pair -name "iPhone"
```

Das Terminal zeigt QR-Code, Link, Code (`XXXX-XXXX-XXXX-XXXX`) und den Fingerabdruck der Bridge und wartet. In der App öffnet sich beim ersten Start die Kopplung:

1. **QR-Code scannen.** Alternativ den Link aus dem Terminal kopieren und in der App einfügen.
2. **Mikrofon erlauben.** Kontakte sind optional; ohne sie siehst du nur Nummern.

Das Terminal meldet danach **Gekoppelt** mit Name, Modell und Schlüssel-Fingerabdruck des iPhones. Ist das nicht dein Gerät: `housephone-bridge devices remove <id>` (steht in der Ausgabe). Strg-C vor dem Koppeln macht den Code ungültig.

Unter **Einstellungen** steht danach **Verbunden** (grün). Steht dort „FRITZ!Box nicht angemeldet“, stimmt Schritt 1 noch nicht.

Die Uhr von iPhone und Server dürfen höchstens 60 s auseinanderliegen (automatische Zeit am iPhone und NTP auf dem Server genügen), sonst lehnt die Bridge Anfragen mit `clock_skew` ab.

### Apple Watch koppeln

Voraussetzung: Housephone ist auf der Watch installiert und das iPhone ist mit der Bridge gekoppelt (**Verbunden**).

1. iPhone: **Einstellungen → Apple Watch koppeln**.
2. Ist Housephone auf der Watch offen, koppelt sie sich sofort. Sonst zeigt das iPhone „Öffne Housephone auf deiner Apple Watch“ – dann die App auf der Uhr öffnen. Der Code gilt 10 Minuten.
3. Auf der Watch **Mikrofon erlauben** antippen.

Unter **Einstellungen → Apple Watch** steht danach **Gekoppelt** (grün). Auf der Uhr zeigt die Startseite **Bereit für Anrufe**.

Die Uhr spricht mit der Bridge nur über den Cloudflare Tunnel – ohne zusätzliche Portfreigabe. Der Ton läuft während eines Anrufs über dieselbe verschlüsselte Verbindung (G.711, Schmalband).

### Entkoppeln

- **iPhone:** Bei **Einstellungen → Kopplung aufheben** wird die Apple Watch mit entkoppelt.
  - Ist Housephone auf der Uhr gerade nicht offen, passiert das beim nächsten Start der Uhr-App, spätestens beim nächsten Anruf.
  - Koppelst du die Uhr vorher neu, verfällt der alte Entkoppel-Auftrag.
- **Nur die Uhr:** Auf der Watch-Startseite ganz unten **Kopplung aufheben**. Das iPhone bleibt gekoppelt.

## 4. Telefonbuch und Anrufliste

Optional, aber praktisch. iPhone und Watch zeigen dann:
- das Telefonbuch der FRITZ!Box, mit den Favoriten oben,
- die Anrufliste deines Anschlusses, auch mit Anrufen am Schnurlostelefon und auf dem Anrufbeantworter.

Eingehende Anrufe tragen den Namen aus dem FRITZ!Box-Telefonbuch. Die Bridge liest dafür nur mit, sie ändert nichts in der FRITZ!Box.

1. **FRITZ!Box-Benutzer anlegen:** **System → FRITZ!Box-Benutzer → Benutzer hinzufügen**.
   - Name z. B. `housephone`, langes Kennwort.
   - Als Recht **nur** „Sprachnachrichten, Faxnachrichten, FRITZ!App Fon und Anrufliste“ ankreuzen.
   - „Zugang auch aus dem Internet erlaubt“ **aus** lassen.
2. **Zugriff für Anwendungen:** **Heimnetz → Netzwerk → Netzwerkeinstellungen → „Zugriff für Anwendungen zulassen“** muss an sein (TR-064; bei FRITZ!OS standardmäßig an).
3. **Bridge:** In `config.yaml` den Abschnitt `fritzbox` mit diesem Benutzernamen ergänzen. Das Kennwort gibst du wie das SIP-Kennwort als Datei oder Umgebungsvariable mit, nie in der Konfigurationsdatei. Genaue Schlüssel siehe [`bridge/README.md`](../bridge/README.md). Danach `docker compose up -d`.
4. **Prüfen:** In der iPhone-App unter **Einstellungen → Bridge** steht **Telefonbuch & Anrufliste: Verbunden**.
   - Kurz nach dem Start kann dort noch „Nicht eingerichtet“ stehen. Dann einmal die App schließen und wieder öffnen.

In der App:
- **Kontakte:** Oben wechselst du zwischen **FRITZ!Box** und **iPhone**.
- **Anrufe:** Unter der Leiste wechselst du zwischen **Housephone** und **FRITZ!Box**.
- **Watch:** Die Uhr zeigt das Telefonbuch unter **Kontakte** und auf der Startseite die verpassten Anrufe der letzten 24 Stunden.

Beides ist auch offline verfügbar, dann mit dem letzten Stand und einem Hinweis.

## 5. Ausprobieren

- [ ] **Ausgehend (WLAN):** Nummer im Tastenfeld wählen → Freiton → Gespräch → Auflegen.
- [ ] **Eingehend, App offen:** Festnetz von einem anderen Telefon anrufen → CallKit klingelt → Annehmen → Ton in beide Richtungen.
- [ ] **Eingehend, gesperrt, unterwegs:** WLAN am iPhone **aus**, App im App-Umschalter schließen, iPhone sperren, Festnetz anrufen → klingelt wie ein normaler Anruf → Annehmen → Ton.
- [ ] **Rückruf aus der iPhone-Telefon-App:** Den Anruf in *Telefon → Anrufliste* antippen → startet über Housephone.
- [ ] **Anderes Telefon nimmt an:** Festnetz anrufen und am Schnurlostelefon abheben → iPhone hört auf zu klingeln („an anderem Gerät angenommen“).

**Apple Watch:**

- [ ] **Eingehend:** Festnetz anrufen → iPhone **und** Watch klingeln → an der Watch annehmen → Gespräch über Lautsprecher und Mikrofon der Uhr in beide Richtungen → das iPhone hört auf zu klingeln.
- [ ] **Ohne iPhone:** iPhone ausschalten oder außer Reichweite, Watch im WLAN oder mit LTE → Festnetz anrufen → Watch klingelt → Gespräch.
- [ ] **Ausgehend:** Auf der Watch **Wählen** → Nummer → grüner Hörer → Freiton → Gespräch → Auflegen.
- [ ] **Lautstärke:** Während des Gesprächs die Digital Crown drehen.
- [ ] **Woanders angenommen:** Festnetz anrufen → Watch klingelt → am iPhone oder am Schnurlostelefon annehmen → die Watch hört nach spätestens 2–3 s auf zu klingeln.
- [ ] **Anrufer legt auf:** Festnetz anrufen → Watch klingelt → Anrufer legt vor dem Annehmen auf → die Watch hört auf zu klingeln und zeigt den Anruf als verpasst.
- [ ] **Entkoppeln:** Auf dem iPhone *Kopplung aufheben* → iPhone und Watch zeigen den Kopplungshinweis; Festnetzanrufe klingeln auf keinem der beiden mehr.

**Telefonbuch und Anrufliste** (wenn eingerichtet):

- [ ] **Kontakte → FRITZ!Box:** Das Telefonbuch erscheint mit den Favoriten oben.
  - Suche nach Name und nach Ziffern.
  - Ein Kontakt mit mehreren Nummern bietet eine Auswahl an; ein Tipp ruft an.
- [ ] **Anrufe → FRITZ!Box:** Einen Anruf am Schnurlostelefon annehmen. Nach dem Aktualisieren (nach unten ziehen) steht er dort mit „am <Mobilteil>“.
  - Verpasste Anrufe sind rot, Anrufbeantworter-Anrufe haben ein eigenes Symbol.
- [ ] **Name beim Anruf:** Von einer Nummer anrufen, die nur im FRITZ!Box-Telefonbuch steht (nicht in den iPhone-Kontakten). Der CallKit-Bildschirm zeigt den Namen.
- [ ] **Watch:** **Kontakte** → Eintrag antippen → Anruf. Ein verpasster Festnetzanruf erscheint auf der Startseite unter **Verpasst** und lässt sich zurückrufen.
- [ ] **Offline:** Flugmodus an → Kontakte → FRITZ!Box zeigt den letzten Stand mit Hinweis „Bridge nicht erreichbar“.

## 6. Wenn etwas nicht klappt

- **Zuerst:** `./housephone tui`, Taste `6` (Selbsttest).
- **Server-Seite:** siehe Tabelle *Fehlersuche* in [`bridge/README.md`](../bridge/README.md).
  - `docker compose logs -f housephone-bridge` zeigt Registrierung, Pushes, Anrufe und ICE.
  - Mit `HOUSEPHONE_LOG_LEVEL=debug` wird es ausführlicher.
- **App-Seite:** Konsole.app auf dem Mac, iPhone auswählen, nach Subsystem `com.jorisconrad.housephone` filtern.
  - Kategorien: `calls` (CallKit/PushKit), `media` (WebRTC/ICE), `bridge` (Verbindung), `watch` (Kopplung der Uhr).
- **Watch-Seite:** In Konsole.app die Watch auswählen, Subsystem `com.jorisconrad.housephone.watch`.
  - Kategorien: `calls`, `audio`, `bridge`, `phone`.
- **Typische Ursachen:**

| Symptom | Ursache |
|---|---|
| Klingelt nur bei offener App | APNs-Key, Key-ID oder Team falsch; Log `push failed` auf der Bridge |
| Klingelt, aber kein Ton unterwegs | UDP-Freigabe 50000 fehlt oder öffentliche IP falsch |
| „Bridge nicht erreichbar“ | Cloudflare Tunnel läuft nicht, oder `publicUrl` in `config.yaml` stimmt nicht mit dem Hostnamen überein |
| Watch zeigt „Wartet auf Push-Freigabe“ | Die Uhr hat noch kein VoIP-Token. Watch-App einmal öffnen; *Push Notifications* für `com.jorisconrad.housephone.watchkitapp` prüfen |
| Watch zeigt „Bridge nicht erreichbar“ | Die Uhr erreicht `phone.<deine-domain>` nicht. Sie versucht es automatisch erneut, auch beim nächsten Öffnen der App |
| Watch klingelt, Annehmen endet mit „Fehlgeschlagen“ | Die Uhr konnte in 10 s keine Verbindung aufbauen. Logs der Bridge (`call.media`) und der Uhr (`calls`) prüfen |
| „Telefonbuch & Anrufliste: Nicht eingerichtet“ | Abschnitt `fritzbox` in `config.yaml` fehlt, oder die Bridge wurde danach nicht neu gestartet |
| Kontakte → FRITZ!Box: „FRITZ!Box nicht verbunden“ | Benutzer, Kennwort oder Recht in der FRITZ!Box falsch, oder „Zugriff für Anwendungen zulassen“ ist aus. Die Meldung darunter kommt von der Bridge; Details im Bridge-Log |
| Watch klingelt weiter, obwohl woanders angenommen | Die Uhr fragt beim Klingeln alle 2 s `GET /v1/calls/{id}` ab. Erreicht sie die Bridge nicht (Log `Call status unavailable` in der Kategorie `bridge`), hört sie erst nach 60 s auf |
