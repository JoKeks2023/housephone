# Housephone Bridge

Die Bridge verbindet deine FRITZ!Box mit der Housephone-App auf iPhone und
Apple Watch. Anrufe klingeln per Push, auch unterwegs und ohne VPN.

> [!IMPORTANT]
> Add-ons gibt es nur mit **Home Assistant OS** oder **Supervised**. Mit
> Home Assistant Container oder Core nimmst du stattdessen Docker Compose
> (siehe README im Repository). Beides nutzt dieselbe Bridge.

## Was du brauchst

1. **IP-Telefon in der FRITZ!Box**: Telefonie → Telefoniegeräte → Neues
   Gerät einrichten → Telefon (mit und ohne Schnurlos) → LAN/WLAN (IP-Telefon).
   Benutzername und Kennwort kommen in die Add-on-Optionen.
2. **Portfreigabe UDP 50000** in der FRITZ!Box auf deinen Home-Assistant-Rechner
   (Internet → Freigaben → Portfreigaben). Darüber läuft der Ton.
   Keine Freigabe für 8080, 8081 oder 8099!
3. **Cloudflare Tunnel** über das Community-Add-on „Cloudflared“, damit die
   App die Bridge unterwegs erreicht (siehe unten).
4. **APNs-Schlüssel** (.p8) aus deinem Apple-Developer-Konto für die
   VoIP-Pushes. Lege ihn in den Konfigurationsordner des Add-ons
   (`/addon_configs/<id>_housephone_bridge/` über Samba oder den
   File-Editor) und trage `/config/AuthKey_XXXX.p8` als *APNs-Schlüssel* ein.

## Cloudflare Tunnel

Der Tunnel steckt nicht im Add-on. Nimm das Cloudflared-Add-on und trage
unter `additional_hosts` die Bridge ein:

```yaml
additional_hosts:
  - hostname: phone.example.com
    service: http://172.30.32.1:8080
```

Die Bridge lauscht für den Tunnel nur auf `172.30.32.1` (dem Home-Assistant-
Netz), nicht im LAN. *Öffentliche URL* ist dann
`wss://phone.example.com/v1/ws`.

Über den Tunnel läuft nur Telefonie gekoppelter Geräte. Koppeln geht nur im
Heimnetz (oder per Tailscale), auf Port 8081. Anfragen aus dem
Home-Assistant-Netz (172.30.32.0/23), also von anderen Add-ons wie
Cloudflared, weist dieser Zugang ab.

## Dashboard

Nach dem Start erscheint **Housephone** in der Seitenleiste, nur für
Administratoren. Home Assistant übernimmt die Anmeldung; die Bridge nimmt
Verbindungen auf dem Dashboard-Port nur vom Ingress-Proxy des Supervisors an.

Das Dashboard zeigt:

- Anmeldung an der FRITZ!Box, öffentliche IP und woher sie kommt (UPnP/STUN)
- Tunnel- und Heimnetz-Zugang, ob Push eingerichtet ist
- Geräte online, laufende Anrufe (Nummern gekürzt wie im Log)
- den Fingerabdruck der Bridge

Und du kannst dort:

- **Kopplungsanfragen freigeben**: Im Heim-WLAN findet die App die Bridge
  von selbst (Bonjour) und zeigt einen sechsstelligen Code. Derselbe Code
  erscheint hier oben unter „Kopplungsanfragen aus dem Heimnetz“. Nur
  freigeben, wenn beide Codes gleich sind.
- **ein Gerät mit QR-Code koppeln**: „Code erzeugen“ zeigt QR-Code und Code.
  Das iPhone muss dafür im Heim-WLAN oder per Tailscale verbunden sein. Die
  Seite meldet, welches Gerät den Code benutzt hat.
- Geräte umbenennen und entfernen (sofort getrennt, laufende Anrufe enden;
  über das Gerät gekoppelte Uhren werden mit entfernt, wenn du nichts anderes
  wählst).

> [!NOTE]
> Das Dashboard ist über jeden Weg erreichbar, über den du Home Assistant
> erreichst, also auch über Nabu Casa. Wer Home-Assistant-Administrator ist,
> kann damit Geräte koppeln. Das Koppeln selbst klappt trotzdem nur, wenn das
> iPhone im Heimnetz oder per Tailscale verbunden ist.

## Optionen

| Option | Bedeutung |
| --- | --- |
| Name der Bridge | Anzeige in der App |
| Öffentliche URL | `wss://…/v1/ws` über den Tunnel |
| Heimnetz-URL | leer = automatisch aus der LAN-IP; mit Tailscale die Tailscale-IP |
| Tunnel-Port | Port auf 172.30.32.1 für Cloudflared (Standard 8080) |
| Tailscale erlauben | Koppeln und Direktverbindung auch über Tailscale |
| Im Heimnetz ankündigen (Bonjour) | Die App findet die Bridge im WLAN ohne QR-Code (Standard: an) |
| FRITZ!Box-Adresse | LAN-IP der FRITZ!Box; die Bridge startet nur mit einer Adresse im Heimnetz |
| IP-Telefon | Benutzername und Kennwort aus der FRITZ!Box |
| FRITZ!Box-Benutzer | optional, für Telefonbuch und Anrufliste |
| APNs | Schlüsseldatei, Key ID, Team ID, Topic |
| Log-Level | `info` reicht; Nummern stehen immer gekürzt im Log |

Kennwörter stehen nur in den Add-on-Optionen und nie im Log.

## Daten und Sicherung

Gekoppelte Geräte und der Schlüssel der Bridge (`identity.key`) liegen in
`/data` und sind in jeder Home-Assistant-Sicherung enthalten. Geht der
Schlüssel verloren, müssen alle Geräte neu gekoppelt werden.

## Ports

| Port | Wo | Wofür |
| --- | --- | --- |
| UDP 50000 | LAN, **freigeben** | Ton zu den Geräten |
| TCP 8081 | LAN | Heimnetz-Zugang: Koppeln, direkte Verbindung zu Hause |
| TCP 8080 | nur 172.30.32.1 | Cloudflare Tunnel |
| TCP 8099 | nur 172.30.32.1 | Dashboard (Ingress) |
| UDP 5062, 40000–40199 | LAN | SIP und RTP zur FRITZ!Box |
