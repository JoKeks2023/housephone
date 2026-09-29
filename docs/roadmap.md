# Zukunftsplan

**Stand:** 2026-09-29.

- Fertig und gemergt: T-0001 (Bridge + iPhone), T-0002 (Watch), T-0003 (FRITZ!Box-Telefonbuch/-Anrufliste).
- Vortest gegen die echte FRITZ!Box bestanden: eingehend und ausgehend, Ton in beide Richtungen (HPHN-8).

| Reihenfolge | Aufgabe | Huly | Kurz |
|---|---|---|---|
| 1 | Sicherheitsbefunde beheben | HPHN-20 | Widerruf wirkt sofort, Registrar nur im LAN, Limits, Nummern im Log maskiert, Abhängigkeiten aktualisiert |
| 2 | **T-0004 Kopplung und Anmeldung v2** | HPHN-27 | Secure-Enclave-Schlüssel, beidseitige Prüfung der Bridge-Identität (Fingerabdruck im QR-Code), Ende-zu-Ende-verschlüsselte Signalisierung, 80-Bit-Code nur per `docker exec`, Container-Härtung |
| 3 | **T-0005 Admin-TUI** | HPHN-28 | `docker compose exec housephone-bridge housephone-bridge tui`: Status, Geräte, Kopplung mit QR, Anrufe live, Statistiken, Logs, Einstellungen, Selbsttest; spricht über einen Unix-Socket mit der Bridge, kein neuer Port |
| 4 | **T-0006 Home Assistant** | HPHN-29 | MQTT-Discovery (nur ausgehend): Status- und Statistik-Sensoren, Ereignis „Anruf eingehend“ für Automationen; Nummern maskiert |
| 5 | Einrichtung und Praxistest | HPHN-8 | Server, Tunnel, APNs-Key, App auf iPhone und Watch |

## Später (Veröffentlichung, siehe `docs/veroeffentlichung.md`)

| Aufgabe | Huly |
|---|---|
| Push-Relay für selbst gehostete Bridges, mit Ende-zu-Ende-verschlüsselter Push-Nutzlast | HPHN-22 |
| Lizenz: Housephone ohne GPL-Historie | HPHN-23 |
| App Review: Demo-Zugang, Exportfrage, Markenname | HPHN-24 |
| Einfachere Einrichtung: IP-Telefon automatisch per TR-064, Home-Assistant-Add-on, fertige Pakete | HPHN-25 |

## Ideen ohne Ticket

- Halten, Makeln, Weiterleiten (SIP REFER)
- TURN-Fallback für restriktive Mobilfunknetze
- Anrufbeantworter-Nachrichten der FRITZ!Box in der App abspielen
- G.722 auf der Watch (libg722 ist gemeinfrei)
- Liquid-Glass-App-Icon in mehreren Ebenen
- Web-Dashboard auf der Admin-API, nur im LAN, mit Passkey
