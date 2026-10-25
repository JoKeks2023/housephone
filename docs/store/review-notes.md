# Notizen für das App-Review

Einfügen unter „App Review Information → Notes“.

**Kurz:** Housephone ist ein Telefon-Client für die eigene Festnetz-Telefonanlage (FRITZ!Box) des Nutzers. Die Gegenstelle, die „Bridge“, betreibt jeder Nutzer selbst zu Hause. Es gibt keinen Server und keine Konten des Anbieters.

**Ohne Hardware testen:** Auf dem ersten Bildschirm „Demo ansehen (ohne FRITZ!Box)“ antippen. Die App zeigt Beispieldaten, Anrufe sind simuliert (Klingeln, dann verbunden, kein Ton, kein Netzwerk). Beenden unter Einstellungen → „Demo beenden“. Ein Anmeldekonto ist nicht nötig.

**Echte Anrufe:** Sie brauchen eine FRITZ!Box mit Festnetzanschluss und die Bridge des Nutzers; beides können wir Prüfern nicht bereitstellen. ‹Optional: Video eines echten Anrufs anhängen, aufgenommen mit eigener Bridge. Ohne Nummern im Bild.›

**Hintergrundmodi und Berechtigungen**
- *Voice over IP / PushKit:* Eingehende Festnetzanrufe kommen als VoIP-Push an. Jeder Push wird sofort bei CallKit als Anruf gemeldet (kein Missbrauch für andere Zwecke). Die Anrufdaten im Push sind Ende-zu-Ende für das Gerät verschlüsselt.
- *CallKit:* Alle Anrufe laufen über CallKit, ausgehend wie eingehend.
- *Audio:* Gesprächston während eines Anrufs.
- *Mikrofon:* Für das Gespräch.
- *Kamera:* Nur zum Scannen des Kopplungs-QR-Codes der Bridge.
- *Kontakte:* Anzeige und Anruferkennung, optional.
- *Lokales Netzwerk / Bonjour:* Findet die eigene Bridge und FRITZ!Box im Heimnetz.
- *Face ID:* Bestätigt Verwaltungsaktionen der Bridge.

**Push-Relay:** VoIP-Pushes kann nur der App-Anbieter senden. Die Bridge des Nutzers schickt ein versiegeltes Paket an ein einfaches Relay des Anbieters, das es an Apple weitergibt. Das Relay speichert nichts und kann Nummer und Name nicht lesen.

**Verschlüsselung:** Standardverfahren (TLS, DTLS-SRTP, Apple CryptoKit/Secure Enclave). Keine eigene Kryptographie, keine proprietären Algorithmen. Export-Compliance: ‹nach Prüfung bestätigen›.

**Marke:** „FRITZ!Box“ kommt nur beschreibend vor, nicht im Namen oder Symbol.

**Kontakt:** ‹Name, E-Mail, Telefon›
