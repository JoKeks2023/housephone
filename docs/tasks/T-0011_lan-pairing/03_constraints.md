# Rahmen

- „Bombensicher“ (ADR-0004): kein Koppeln ohne Admin, kein Raten des Codes ohne Commitment, nur privater Listener, Ablauf und Grenzen.
- Bonjour verrät nichts Geheimes (Name, Protokoll, 8 Zeichen Fingerabdruck).
- Keine echten Telefonnummern, keine Huly-Links.
- Kein Simulator, keine lokalen Xcode-Builds (M1, 8 GB); der App-Build läuft in der CI.
- Texte in den String-Katalogen im Xcode-Format (`"key" : {`), nur Einfügungen.
