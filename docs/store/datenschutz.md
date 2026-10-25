# Datenschutz

## Antworten in App Store Connect („App-Datenschutz“)

- **Tracking:** Nein.
- **Erhobene Daten:** *Geräte-ID* (das Push-Gerätetoken). Nicht mit der Identität verknüpft, nicht zum Tracking, Zweck „App-Funktionalität“. Alles andere: nicht erhoben.
- Begründung: Nummern, Namen, Kontakte und Gesprächsdaten gehen nur an die eigene Bridge des Nutzers, die der Anbieter weder betreibt noch einsehen kann. Nur das Gerätetoken läuft zum Push-Relay des Anbieters (nur im Arbeitsspeicher, kein Log, keine Datenbank).
- Passt zu `PrivacyInfo.xcprivacy` in iPhone- und Watch-App.

## Datenschutzerklärung (Entwurf, auf öffentliche Seite stellen)

**Verantwortlicher:** ‹Name, Anschrift, E-Mail› (Impressum dort verlinken)

**Was Housephone verarbeitet**
- *Auf deinem iPhone und deiner Watch:* Zugangsdaten zu deiner Bridge (im Schlüsselbund), Favoriten, Anrufliste, optional deine Kontakte. Diese Daten verlassen dein Gerät nicht in Richtung Anbieter.
- *Zwischen App und deiner Bridge:* Rufnummern, Namen, Gesprächston, Telefonbuch und Anrufliste deiner FRITZ!Box. Die Bridge betreibst du selbst. Der Anbieter hat keinen Zugriff.
- *Push-Benachrichtigung bei eingehenden Anrufen:* Damit dein iPhone klingelt, sendet deine Bridge ein Paket an das Push-Relay des Anbieters, das es an Apple (APNs) weitergibt. Das Relay sieht dein Gerätetoken, den Zeitpunkt und die Adresse deiner Bridge. Rufnummer und Name sind für dein Gerät verschlüsselt und für Relay und Apple unlesbar. Das Relay speichert nichts dauerhaft und führt kein Zugriffsprotokoll. Der Zugang ins Internet läuft über Cloudflare; Cloudflare als Auftragsverarbeiter sieht dabei Gerätetoken und Bridge-Adresse, nicht Nummer oder Name.
- *Kein Tracking, keine Werbung, keine Analyse, keine Absturzberichte an den Anbieter.*

**Rechtsgrundlage:** Vertragserfüllung bzw. Bereitstellung der App-Funktion (Art. 6 Abs. 1 lit. b DSGVO) für das Gerätetoken.

**Speicherdauer:** Das Gerätetoken wird nicht gespeichert, nur zur Weiterleitung verwendet.

**Deine Rechte:** Auskunft, Berichtigung, Löschung, Einschränkung, Widerspruch, Beschwerde bei einer Aufsichtsbehörde. Kontakt: ‹E-Mail›.

**Löschen:** App löschen oder in den Einstellungen die Kopplung aufheben; Daten bei dir und auf deiner Bridge liegen in deiner Hand.
