# ADR-0009: Verwaltung in der iPhone-App (Admin-Rolle und Face ID)

- Status: vorgeschlagen (2026-10-01), Branch `feat/app-admin` (HPHN-37)
- Ergänzt ADR-0004 (Anmeldung v2), ADR-0007 (Koppeln im Heimnetz), ADR-0008 (Profile).

## Kontext

- Verwalten ging bisher nur auf dem Server: CLI, TUI über `/data/admin.sock`, im Home-Assistant-Add-on das Dashboard.
- Vorgabe des Nutzers (2026-09-29): Verwaltung auch aus der App, aber **nur im Heimnetz** (oder über Tailscale), nie über den Cloudflare Tunnel.
- Ein entsperrtes iPhone allein soll nicht reichen: Wer das Telefon samt Code in der Hand hat, soll die Bridge nicht umbauen können.

## Entscheidung

1. **Nur auf dem privaten Listener.** `/v1/admin/*` gibt es nur auf `bridge.privateListen` (HPHN-40). Dessen Quellfilter lässt nur RFC 1918, ULA, Link-Local und optional Tailscale durch; Loopback zählt nicht, weil `cloudflared` von dort kommt. Im Add-on schließt `excludedNetworks` die anderen Add-ons aus. Auf dem öffentlichen Listener antwortet jeder Admin-Pfad `403 home_network_required`, ebenso ohne Admin-Backend.
2. **Zwei Signaturen pro Anfrage.**
   - Wie jede Geräteanfrage: `Authorization: HP2 …` mit dem Geräteschlüssel, Antwort signiert und versiegelt (ADR-0004). Im LAN ohne TLS ist das Ende-zu-Ende.
   - Zusätzlich `HP2-Admin: <sig>`: ECDSA P-256 (raw `r‖s`, base64url) des **Admin-Schlüssels** über `HP2-ADMIN\nMETHOD\npath?query\nbridgeId\ndeviceId\nts\nnonce\nepk\nsha256hex(body)`. Dieselben Felder wie `HP2-AUTH`, anderes Label: Keine Signatur gilt für die andere.
   - Replay: Die `nonce` ist an die Gerätesignatur gebunden und wird pro Gerät 120 s gemerkt; eine mitgeschnittene Admin-Anfrage scheitert mit `401`.
3. **Admin-Schlüssel mit Face ID.** Ein zweiter Secure-Enclave-Schlüssel, erzeugt mit `.privateKeyUsage` + `.biometryCurrentSet`, `WhenPasscodeSetThisDeviceOnly`. Er signiert nur nach Face ID bzw. Touch ID der aktuell eingerichteten Gesichter/Finger; ein neu eingerichtetes Face ID macht ihn unbrauchbar. Der Geräteschlüssel bleibt ohne Biometrie (Anrufe im Sperrbildschirm).
4. **Rolle und Einrichtung.**
   - `devices.json` bekommt `admin`, `adminKey`, `adminEnrollUntil`.
   - **Frische Installation:** Das erste Gerät der Bridge wird Admin, wenn es ein iPhone ist und mit einem Code des Servers gekoppelt wurde (QR oder Heimnetz-Freigabe). Prüfung und Schreiben passieren unter einem Lock.
   - **Weitere Admins** nur ausdrücklich: `devices promote <id>`, TUI-Taste `a`, Dashboard „Zum Admin machen“ oder ein Admin-iPhone. Entziehen: `devices demote`, TUI `A`, Dashboard, Admin-iPhone.
   - **Einrichten des Schlüssels:** Eine Beförderung öffnet ein Fenster von 1 h. Darin meldet das iPhone `POST /v1/admin/enroll` mit `{adminKey, proof}`; `proof` ist die Signatur des Admin-Schlüssels über `HP2-ADMIN-ENROLL\nbridgeId\ndeviceId\nadminKey` (Besitznachweis). Die Anfrage selbst ist mit dem Geräteschlüssel signiert. Mit der Einrichtung schließt das Fenster.
   - **Neuer Schlüssel** (z. B. nach geändertem Face ID) nur nach erneuter Beförderung. So kann jemand mit Telefon und Code, aber ohne das eingerichtete Gesicht, keinen eigenen Admin-Schlüssel nachschieben. Bis dahin bleibt der alte Schlüssel gültig.
   - Eine **Apple Watch** ist nie Admin: Beförderung `not_allowed`, Einrichtung `admin_enroll_closed`, Admin-Anfragen `admin_required`, auch wenn `devices.json` anderes behauptet.
   - Die Rolle wird bei **jeder** Anfrage aus `devices.json` gelesen; Entziehen wirkt sofort.
5. **Sichtbarkeit.** Jede Admin-Aktion (aus der App, CLI, TUI oder Dashboard) geht als `admin.action {actor, action, target?, at}` an alle verbundenen Geräte und ins Log. `target` (Gerätename) bekommen nur Geräte desselben Profils und Admins (ADR-0008); die anderen sehen, dass etwas passiert ist. Das betroffene Gerät bekommt bei Beförderung/Entzug `admin.role`; `welcome.admin` trägt die Rolle beim Verbinden.
6. **Funktionen** (dieselbe `admin.Service` wie TUI und Dashboard): Status (Anmeldung pro Profil, öffentliche IP und Quelle, Push, Geräte, laufende Anrufe), Statistik, Geräte pro Profil, Umbenennen, Entfernen (mit Uhren), Verschieben, Befördern/Entziehen, Kopplungsanfragen aus dem Heimnetz mit SAS und Profilwahl freigeben/ablehnen, QR-Einladung mit Profil. Einstellungen ändern bleibt außen vor.
7. **App.**
   - „Verwaltung“ in den Einstellungen nur für Admins; benutzbar nur, wenn die Verbindung gerade über den LAN-Listener läuft, sonst ausgeblendet mit „nur im Heimnetz“.
   - Lesen (Status, Geräte, Anfragen) teilt sich eine Face-ID-Prüfung für höchstens 2 min (ein `LAContext`), verworfen beim Verlassen des Bildschirms oder wenn die App in den Hintergrund geht. Jede Änderung fragt neu nach Face ID; Entfernen und Rechte-Entzug zusätzlich mit Rückfrage.
   - Wird ein iPhone demoted, löscht die App ihren Admin-Schlüssel.

## Bestehende Installationen

Gibt es schon Geräte, wird niemand automatisch Admin: Der Haushalt hat vielleicht mehrere iPhones, und das älteste muss nicht das des Admins sein. Einmalig auf dem Server `./housephone devices promote <id>` (oder TUI `a`, Dashboard), dann innerhalb einer Stunde in der App „Face ID für die Verwaltung einrichten“.

## Sicherheitsbetrachtung (gegen ADR-0004/0007)

- **Angreifer im Internet:** erreicht nur den öffentlichen Listener; dort gibt es keine Admin-Pfade.
- **Angreifer im WLAN ohne Gerät:** braucht Geräte- und Admin-Schlüssel; beide verlassen die Secure Enclave nicht.
- **Angreifer mit entsperrtem iPhone eines Admins:** Lesen und Ändern scheitern an Face ID; einen eigenen Admin-Schlüssel kann er nur in einem offenen Fenster einrichten, das nur ein anderer Admin oder der Server öffnet.
- **Angreifer mit einem normalen Gerät:** `admin_required`, der Dienst wird nicht aufgerufen.
- **Mitschnitt im LAN:** Antworten sind versiegelt, Anfragen wegen der Nonce nicht wiederholbar; eine Signatur für einen anderen Pfad oder Body passt nicht.
- **Restrisiko:** Ein Admin, der befördert wird, während ein Angreifer sein entsperrtes iPhone hat, könnte in diesem Fenster den Schlüssel des Angreifers bekommen. Das Fenster ist eine Stunde lang und jede Einrichtung wird allen Geräten gemeldet.

## Folgen

- Testvektoren `docs/protocol/fixtures/crypto/admin-vectors.json` (Go-Generator `bridge/cmd/_adminvectors`), geprüft in Go und Swift.
- Nicht geprüft: das Verhalten von Face ID mit einem wiederverwendeten `LAContext` (eine Abfrage für mehrere Lese-Anfragen) und die Oberfläche auf einem echten iPhone.
