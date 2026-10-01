# T-0011: Koppeln im Heimnetz ohne QR-Code (HPHN-41)

Die App findet die Bridge im Heim-WLAN per Bonjour (`_housephone._tcp`) und zeigt „Bridge ‚Zuhause' gefunden – koppeln?“. Beide Seiten zeigen einen sechsstelligen Bestätigungscode; ein Admin vergleicht und gibt in TUI, CLI oder HA-Dashboard frei. Das Gerät pinnt den Bridge-Schlüssel aus dem signierten Austausch. QR-Kopplung bleibt für das erste Gerät, Tailscale und als Rückfall; die Watch koppelt weiter automatisch.

Design: `docs/architecture/ADR-0007-koppeln-im-heimnetz.md`. Protokoll: `docs/protocol/signaling-v2.md`, Abschnitt „Koppeln im Heimnetz ohne QR-Code (v2.1)“.
