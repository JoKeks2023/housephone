# Randbedingungen

- Mac App Store verlangt App Sandbox: Entitlements `network.client` und `device.audio-input`. Weitere nur mit Begründung.
- Bundle-ID `<präfix>.housephone.mac`, gleicher Eintrag im Store möglich (Universal Purchase) oder getrennt, Entscheidung offen.
- Kein Fremdcode: nur `HousephoneKit` und Apple-Frameworks. WebRTC ist nicht nötig, solange der WebSocket-Audiopfad reicht.
- Lokal keine schweren Builds (8-GB-Mac): Bau-Nachweis über CI.
- Eingehende Anrufe sind nur bei laufender App möglich (kein PushKit auf macOS); die Oberfläche muss das offen sagen.
