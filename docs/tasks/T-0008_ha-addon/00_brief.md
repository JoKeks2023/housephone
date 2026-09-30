# T-0008 · Bridge als Home-Assistant-Add-on (HPHN-51)

Die Bridge soll als Add-on in einer Home-Assistant-Instanz laufen, mit einem Web-Dashboard per Ingress (nur HA-Admins): Status, laufende Anrufe, Geräte, Koppeln.

Entscheidungen des Users:
- Zugriff: „Ingress, nur für HA-Admins“, auch über Nabu Casa erreichbar.
- Das Dashboard gibt es vorerst nur im Add-on. Es wird aber so gebaut, dass es später auch unter Docker Compose läuft (HPHN-39: Heimnetz/Tailscale mit Passkey). Dafür bekommt es eine austauschbare Zugriffsschicht.

Entwurf: ADR-0006.
