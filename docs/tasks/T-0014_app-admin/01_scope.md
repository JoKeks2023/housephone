# Umfang

**Drin:**
- Bridge: `/v1/admin/*` nur auf dem privaten Listener, `HP2-Admin`-Signatur, Admin-Rolle in `devices.json`, Einrichtung mit Fenster, erstes iPhone wird Admin, `admin.action`/`admin.role`, `welcome.admin`
- Befördern/Entziehen in CLI (`devices promote|demote`), TUI (`a`/`A`), Dashboard, Admin-API (Socket)
- Funktionen in der App: Status, Statistik, Geräte pro Profil, umbenennen, entfernen, verschieben, befördern/entziehen, Heimnetz-Anfragen mit SAS und Profil freigeben/ablehnen, QR-Einladung mit Profil
- iOS: Admin-Schlüssel in der Secure Enclave mit `.biometryCurrentSet`, Abschnitt „Verwaltung“ in den Einstellungen, Banner für Admin-Aktionen, `NSFaceIDUsageDescription`, Texte de/en
- Testvektoren Go ↔ Swift, ADR-0009, Protokoll- und README-Doku

**Nicht drin:**
- Einstellungen der Bridge ändern
- Passkey-Web-UI für Docker (HPHN-39)
- Automatische Migration bestehender Installationen (bewusst: Beförderung von Hand)
