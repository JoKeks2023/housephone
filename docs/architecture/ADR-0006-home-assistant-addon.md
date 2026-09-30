# ADR-0006: Bridge als Home-Assistant-Add-on, Web-Dashboard

- Status: vorgeschlagen (2026-09-30), Branch `feat/ha-addon` (HPHN-51)
- Ergänzt ADR-0001 (Bridge-Architektur) und die Zugänge aus `signaling-v2.md`.

## Kontext

- Viele Heimserver laufen schon mit Home Assistant OS. Ein eigener Docker-Compose-Stack ist dort umständlich; Add-ons sind der vorgesehene Weg.
- Die Bridge ist ein einzelnes statisches Go-Programm (amd64/arm64) und passt als Add-on-Container.
- Koppeln lief bisher über `docker compose exec … pair` oder die TUI. In Home Assistant gibt es dafür keine bequeme Shell.

## Entscheidung

1. **Add-on-Repository im selben Git-Repo:** `repository.yaml` und `ha-addon/housephone-bridge/`. Das Image `ghcr.io/jokeks2023/housephone-bridge-addon` baut dieselbe Bridge (Dockerfile-Target `addon`, läuft als root, weil der Supervisor `/data` als root einhängt).
2. **Optionen statt `config.yaml`:** `serve -ha-options /data/options.json` liest die Add-on-Optionen (`config.LoadHAOptions`). Docker Compose bleibt unverändert.
3. **Host-Netzwerk**, damit SIP/RTP, UDP 50000, der Heimnetz-Zugang und UPnP wie bisher funktionieren.
4. **Zugänge im Add-on:**
   - Tunnel: `172.30.32.1:<tunnel_port>` (Gateway des Supervisor-Netzes). Nur der Host und Add-ons erreichen ihn, also auch das Cloudflared-Add-on. Proxy-Header gelten aus `172.30.32.0/23`.
   - Heimnetz: `:8081` wie bisher, aber `172.30.32.0/23` ist ausgeschlossen (`bridge.excludedNetworks`). Kein Add-on und kein falsch eingestellter Tunnel kann darüber koppeln.
   - Dashboard: `172.30.32.1:8099`, Ingress.
5. **Web-Dashboard als eigenes Paket** (`internal/dashboard`): Status, laufende Anrufe (Nummern gekürzt wie im Log), Geräte (umbenennen, entfernen mit Bestätigung), Koppeln mit QR-Code und Rückmeldung, welches Gerät den Code benutzt hat. Serverseitiges HTML, wenig JavaScript, CSP ohne Inline-Skripte, CSRF-Token für jeden POST.
6. **Zugriff ist austauschbar (`dashboard.Gate`):** Die Seiten wissen nicht, wer sie benutzen darf und unter welchem Pfad sie liegen.
   - Heute: `SupervisorGate`. Nur der Ingress-Proxy `172.30.32.2` darf verbinden (TCP-Absender, keine Header). Home Assistant hat angemeldet; `panel_admin: true` beschränkt auf Admins. Der Pfad kommt aus `X-Ingress-Path` (geprüft).
   - Später (HPHN-39): ein Passkey-Gate für Docker Compose auf dem Heimnetz-Zugang. Es implementiert dasselbe Interface; die Seiten bleiben gleich.
7. **Das Dashboard startet nur im Add-on-Modus** (`app.WithDashboard`, nur von `serve -ha-options` gesetzt). Ohne Add-on gibt es keinen Dashboard-Listener und keine Option dafür in `config.yaml`.
8. **Kein gebündelter Tunnel:** Das Community-Add-on „Cloudflared“ zeigt auf `http://172.30.32.1:8080`.

## Folgen

- Das Dashboard ist so erreichbar wie Home Assistant selbst, also auch über Nabu Casa. Entschieden: Das ist in Ordnung, weil nur HA-Admins es sehen. Das Koppeln selbst verlangt weiterhin, dass das Gerät im Heimnetz oder per Tailscale verbunden ist.
- Add-ons gibt es nur mit Home Assistant OS oder Supervised. Home Assistant Container nutzt Docker Compose.
- Das Add-on-Image wird bei jedem Push auf `master` unter der Version aus `config.yaml` neu gebaut. Ein Update für Nutzer gibt es erst, wenn diese Version steigt.
- Die Supervisor-Adressen (`172.30.32.1`, `172.30.32.2`, `172.30.32.0/23`) sind fest einkompiliert. Ändert Home Assistant sie, braucht die Bridge ein Update.
