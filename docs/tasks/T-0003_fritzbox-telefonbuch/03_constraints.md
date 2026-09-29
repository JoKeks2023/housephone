# Randbedingungen

Es gelten die Randbedingungen aus T-0001 und T-0002. Zusätzlich:

- **FRITZ!Box-Zugangsdaten** liegen nur auf der Bridge, per Umgebungsvariable oder Datei, nie im Repo.
- **Nur lesend:** keine Aktionen, die in der FRITZ!Box etwas verändern.
- **Keine Live-Tests** gegen die echte FRITZ!Box mit Anmeldung ohne Zustimmung des Nutzers.
  - Nicht angemeldete Leseabfragen wie `tr64desc.xml` oder `GetSecurityPort` sind in Ordnung.
  - Hintergrund: Fehlgeschlagene Anmeldungen landen im Ereignisprotokoll der FRITZ!Box.
