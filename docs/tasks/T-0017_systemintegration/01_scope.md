# Umfang

Die Arbeit läuft in Phasen, jede Phase ist ein eigener PR.

| Phase | Inhalt | Apple-Freigabe nötig? |
|:-:|---|---|
| 1 | **Grundlage:** App Group `group.<präfix>.housephone` mit einem Schnappschuss für Erweiterungen (Favoriten, letzte Anrufe, „eingerichtet“), abgesicherte Deep Links, App Intents (Favorit anrufen, Nummer anrufen, Tastenfeld, verpasste Anrufe), App Shortcuts für Siri und Kurzbefehle, Siri-Anrufe per Sprache (`INStartCallIntent`, In-App-Handler), Schnellaktionen am Home-Bildschirm (Favoriten) | Nein, Siri und App Group gehen mit automatischer Signierung |
| 2 | **Widgets (iPhone):** Favoriten (klein/mittel), letzte und verpasste Anrufe, Sperrbildschirm-Widgets, Kontrollzentrum- und Action-Button-Schalter (iOS 18) | Nein |
| 3 | **Watch:** Komplikationen und Smart-Stack-Widgets (Favorit, verpasste Anrufe), Favoriten vom iPhone, Siri auf der Watch (Favoriten per App Shortcuts) | Nein |
| 4 | **CarPlay:** Kommunikations-App mit Favoriten, Anrufliste und FRITZ!Box-Telefonbuch als Listen; Anrufen per Tippen und Siri | **Ja:** Entitlement `com.apple.developer.carplay-communication` auf Antrag. Bis dahin baut die App ohne das Entitlement (Schalter in der xcconfig) |

Bewusst nicht im Umfang:
- **Live Activity / Dynamic Island für laufende Anrufe:** CallKit zeigt das bereits systemweit. Eine zweite Anzeige wäre doppelt.
- **Fokus-Filter:** erst nach Bedarf.
