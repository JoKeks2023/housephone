# Changelog

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
