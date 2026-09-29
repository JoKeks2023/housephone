# Randbedingungen

- **Nur Standard-Kryptografie:** CryptoKit auf Apple-Seite, Go-Standardbibliothek plus `golang.org/x/crypto`. Keine eigenen Primitive.
- **Signaturen prüfen:** nur mit den Verifikationsfunktionen der Bibliotheken, keine Vergleiche mit selbst gebauten Abkürzungen.
- **Geräteschlüssel:** Signieren muss bei gesperrtem Gerät funktionieren (eingehender Anruf): `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`, keine Biometrie.
- **Keine Geheimnisse** in Logs, Tickets oder Commits. Das gilt auch für echte Telefonnummern (Nutzervorgabe).
- **Lokal:** kein Simulator, kein lokales `xcodebuild`; die CI baut.
