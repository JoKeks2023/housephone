# T-0015: Push-Relay (HPHN-22)

Nutzer der App-Store-App sollen keinen APNs-Key brauchen, auch nicht im Home-Assistant-Add-on. Der Key bleibt beim Anbieter: ein Relay per Docker Compose auf dessen VPS (netcup), kein Cloudflare Worker (Vorgabe 2026-10-01). Nummer und Name des Anrufers sollen Relay und Apple nicht lesen können.
