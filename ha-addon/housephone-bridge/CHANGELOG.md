# Changelog

## 0.5.0

- Ton auch ohne Portfreigabe: Kommt unterwegs keine direkte Verbindung
  zustande (UDP 50000 nicht freigegeben), läuft der Ton über den Cloudflare
  Tunnel. Braucht die App ab dieser Version. Mit Portfreigabe bleibt der
  direkte Weg (besserer Ton, weniger Verzögerung).

## 0.4.0

- Von unterwegs einfacher einrichten: Bei „Öffentliche Adresse“ reicht der
  Hostname des Tunnels (z. B. `phone.example.com`), `wss://…/v1/ws` ergänzt die
  Bridge.
- Neue Karte „Von unterwegs“ im Dashboard: prüft, ob die Bridge über den
  Tunnel erreichbar ist, und führt mit Buttons zu Cloudflare und zu den
  Add-on-Optionen. Die Ziel-Adresse für den Tunnel lässt sich kopieren.
- Das Add-on liest per Supervisor-API nur seinen eigenen Slug (für den Link
  zu den Optionen).

## 0.3.1

- Push-Relay unter neuer Adresse `https://push.jorisconrad.com/housephone`.
  Die alte Adresse hatte kein gültiges Zertifikat, Pushes kamen nicht an.

## 0.3.0

- Push ohne eigenen APNs-Schlüssel (HPHN-22): Die Bridge weckt die Geräte
  über das Push-Relay der App. Nummer und Name des Anrufers sind
  Ende-zu-Ende verschlüsselt. Die Optionen für Schlüsseldatei, Key ID und
  Team ID fallen weg, ebenso der Konfigurationsordner.
- Neu und optional: „Push-Relay“ für selbst gebaute Apps.
- Braucht die App ab dieser Version; ältere Apps bekommen über das Relay
  keine Pushes (der Selbsttest zeigt das an).

## 0.2.0

- Mehrere Profile (HPHN-42): jede Person mit eigener Festnetznummer als
  eigenes IP-Telefon (Option „Weitere Profile“). Anrufe, Push, Anrufliste
  und Telefonbücher bleiben im eigenen Profil.
- Dashboard: Profile mit Anmeldestatus, Profil beim Koppeln und Freigeben
  wählen, Geräte zwischen Profilen verschieben.

## 0.1.0

- Erste Version als Home-Assistant-Add-on (HPHN-51).
- Optionen in der Add-on-Oberfläche statt `config.yaml`.
- Dashboard in der Seitenleiste (Ingress, nur Administratoren): Status,
  laufende Anrufe, Geräte, Koppeln per QR-Code.
- Tunnel-Zugang nur auf 172.30.32.1 für das Cloudflared-Add-on; der
  Heimnetz-Zugang weist das Home-Assistant-Netz ab.
