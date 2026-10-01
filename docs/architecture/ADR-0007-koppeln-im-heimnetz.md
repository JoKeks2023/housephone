# ADR-0007: Koppeln im Heimnetz ohne QR-Code (Bonjour + Bestätigungscode)

- Status: vorgeschlagen (2026-10-01), Branch `feat/lan-pairing` (HPHN-41)
- Ergänzt ADR-0004 (Anmeldung v2) und die Zugänge aus `signaling-v2.md` (HPHN-40).

## Kontext

- Bisher braucht jedes iPhone einen QR-Code von `housephone-bridge pair`, der TUI oder dem Dashboard. Darin steckt der Fingerabdruck der Bridge, den das Gerät pinnt (ADR-0004).
- Im Heim-WLAN soll das ohne Code gehen: App öffnen, Bridge antippen, auf der Bridge freigeben.
- Ohne QR-Code fehlt dem Gerät der Fingerabdruck. Wer im WLAN mithört oder sich als Bridge ausgibt (ARP-/mDNS-Spoofing), könnte sich dazwischenschalten. Das muss eine Bestätigung durch einen Menschen abfangen.

## Entscheidung

1. **Bonjour:** Die Bridge kündigt ihren Heimnetz-Zugang als `_housephone._tcp` an (`internal/bonjour`, `hashicorp/mdns`). Nur wenn `privateListen` im Heimnetz erreichbar ist (private oder link-lokale Adresse; nicht Loopback, nicht nur Tailscale). Abschaltbar mit `bridge.bonjour: false` bzw. der Add-on-Option `bonjour`.
2. **TXT-Record ohne Geheimnisse:** `txtvers=1`, `proto=hp2`, `pair=lan`, `fp=<erste 8 Zeichen des Fingerabdrucks>`. Instanzname ist der Bridge-Name.
   - Begründung: Der Fingerabdruck ist der Hash eines öffentlichen Schlüssels. 48 Bit reichen der App, um eine schon gekoppelte Bridge von einer anderen zu unterscheiden; für die Sicherheit spielen sie keine Rolle, die trägt allein der Bestätigungscode. Bridge-ID, URLs und Versionen stehen nicht drin.
3. **Drei Schritte mit Commitment** (nur auf dem privaten Listener; der öffentliche antwortet `403 home_network_required`):
   1. `POST /v1/pair/lan`: Gerät → Bridge: Name, Plattform (`ios`), Modell, Geräteschlüssel `D` (P-256) und `C = SHA-256("HP2-LAN-COMMIT" ‖ D ‖ eD ‖ nD)`. `eD` ist ein frischer X25519-Schlüssel, `nD` 16 Zufallsbytes.
   2. Antwort: `pairingId`, Bridge-ID und -Name, Bridge-Schlüssel `B` (Ed25519), frisches `eB`, `nB`, Ablaufzeit, Signatur von `B` über `HP2-LAN-OFFER` (ID, Bridge-ID, `D`, `C`, `B`, `eB`, `nB`).
   3. `POST /v1/pair/lan/{id}/reveal`: Gerät → Bridge: `eD`, `nD` und ein Beweis (ECDSA mit `D`) über den Transkript-Hash. Die Bridge prüft `C` und den Beweis; jede Abweichung beendet die Anfrage endgültig.
   - Transkript-Hash `T = SHA-256("HP2-LAN-TRANSCRIPT" ‖ ID ‖ Bridge-ID ‖ D ‖ C ‖ B ‖ eB ‖ nB ‖ eD ‖ nD)`.
   - Bestätigungscode `SAS = HKDF-SHA256(X25519(eD, eB), salt = T, info = "HP2-LAN-SAS", 4 Byte)` als Zahl mod 10⁶, sechsstellig. Schlüssel der Freigabe: dasselbe mit `info = "HP2-LAN-SEAL"`, 32 Byte.
   - Alle Felder sind Base64url ohne Padding, Zeilen mit `\n` verbunden, wie in ADR-0004.
4. **Warum Commitment:** Sechs Ziffern sind ~20 Bit. Ohne Commitment könnte ein Angreifer in der Mitte seine Schlüssel so lange variieren, bis beide Seiten denselben Code zeigen. Mit Commitment legt sich jede Seite fest, bevor sie die Zufallswerte der anderen kennt: Gegenüber der Bridge hat sich der Angreifer mit `C'` festgelegt, bevor `nB` kommt; gegenüber dem Gerät muss er `eB'`, `nB'` schicken, bevor `eD`, `nD` aufgedeckt sind. Beide Codes sind für ihn Zufall und stimmen mit Wahrscheinlichkeit 10⁻⁶ überein. Jeder Versuch ist eine sichtbare Anfrage, die ein Admin freigeben müsste. Dasselbe Muster nutzen Bluetooth (Numeric Comparison) und ZRTP.
5. **Freigabe durch einen Admin:** Die Bridge zeigt Gerätename, Modell, Absender-IP, Schlüssel-Kurzfingerabdruck und den Code:
   - TUI, Tab „Kopplung“: `a` freigeben (zweite Taste `j` nach Anzeige des Codes), `d` ablehnen. Andere Tabs zeigen einen Hinweis auf offene Anfragen.
   - CLI: `housephone-bridge devices pending`, `devices approve <id>` (fragt „Zeigt das iPhone genau diesen Code?“; ohne Rückfrage nur mit `-code 123456`, das passen muss), `devices deny <id>`.
   - HA-Dashboard: Karte „Kopplungsanfragen aus dem Heimnetz“ mit Code, „Code stimmt – freigeben“ und „Ablehnen“ (CSRF wie alle POSTs).
   - Admin-iPhone mit Face ID (HPHN-37) und die Wahl des Profils (HPHN-42) kommen später; die Admin-API (`/v1/lan-pairings`) ist dafür die Naht.
6. **Ergebnis:** `GET /v1/pair/lan/{id}?wait=25` (Long-Poll) liefert `pending`, `denied`, `expired` oder `approved`. Die Freigabe enthält Geräte-ID, Bridge-ID und -Name, öffentliche und Heimnetz-URL und eine Signatur von `B` über `HP2-LAN-APPROVED` (T, Geräte-ID, Bridge-ID, URLs); sie ist mit dem Freigabe-Schlüssel versiegelt (ChaCha20-Poly1305, Zähler 0, AAD „HP2“). Das Gerät pinnt `B` aus dem signierten Angebot.
7. **Grenzen:** Anfragen leben nur im Speicher, 2 Minuten ab Start, Ergebnis noch 1 Minute abholbar. Gebunden an die Absenderadresse (bzw. /64). Pro Absender 5 Starts in 10 Minuten, insgesamt 30 pro Stunde, höchstens 5 gleichzeitig offen; ein neuer Start derselben Adresse ersetzt ihren alten. Nur Plattform `ios`; die Apple Watch koppelt weiter automatisch über das iPhone (`pair.companion`).
8. **QR-Code bleibt:** für das erste Gerät, für Tailscale und als Rückfall. In der App gibt es außerdem „Adresse eingeben (Tailscale)“: Bonjour reicht nicht ins Tailnet, der Ablauf ist danach derselbe.

## Folgen

- Testvektoren: `docs/protocol/fixtures/crypto/lan-pairing-vectors.json` (erzeugt von `bridge/cmd/_lanvectors` direkt aus den Primitiven); Go (`internal/hp2`) und Swift (`LanPairingTests`) prüfen dagegen. HTTP-Beispiele: `docs/protocol/fixtures/http/pair-lan.*.json`.
- Die Sicherheit hängt daran, dass der Admin wirklich vergleicht. Die Oberflächen sagen das bei jeder Freigabe; die CLI fragt nach.
- Wer in die Admin-Oberfläche kommt (Shell auf dem Server, HA-Admin), kann jedes Gerät freigeben – wie schon beim QR-Code.
- Bonjour im HA-Add-on läuft im Host-Netzwerk neben dem mDNS von Home Assistant (`SO_REUSEADDR` auf 5353). Auf echter Hardware nicht geprüft.

## Offene Punkte

- Freigabe auf einem Admin-iPhone mit Face ID (HPHN-37), Profilwahl (HPHN-42).
- Erkennen in der App, dass eine gefundene Bridge schon gekoppelt ist (`fp`-Präfix gegen die gespeicherten Zugangsdaten), sobald es dafür einen Einstieg außerhalb des Onboardings gibt.
