# Fertig, wenn

- Bridge ohne Key sendet über das Relay; der Push kommt versiegelt an und lässt sich mit dem Geräteschlüssel öffnen (End-to-End-Test).
- Relay lehnt Klartext, fremde Topics, ungültige Tokens und Fluten ab; tote Tokens melden `410`, die Bridge vergisst sie.
- Go und Swift öffnen dieselben Testvektoren.
- Relay-Image startet gehärtet (read-only, ohne Capabilities) in der CI.
- Add-on hat keine APNs-Felder mehr.
- Auf echtem iPhone über das laufende Relay geklingelt (offen bis zum Deploy).
