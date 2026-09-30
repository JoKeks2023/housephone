# Randbedingungen

- Docker-Compose-Pfad unverändert; das Dashboard startet nur im Add-on-Modus.
- Verwaltung bleibt an eine Anmeldung gebunden: im Add-on durch Home Assistant (nur Admins), Koppeln weiterhin nur aus dem Heimnetz/Tailscale.
- Keine Secrets und keine echten Telefonnummern in Logs, Tests oder Repo.
- Kein lokaler Docker-Build (M1, 8 GB); Images baut die CI.
- Vorgaben des Supervisors: nur Verbindungen von 172.30.32.2 auf dem Ingress-Port, Pfad aus `X-Ingress-Path`.
