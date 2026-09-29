# Randbedingungen

Es gelten die Randbedingungen aus T-0001: kein Simulator, keine schweren lokalen Builds, die CI baut die App. Zusätzlich:

- **Identitäten der Watch:**
  - Bundle-ID `com.jorisconrad.housephone.watchkitapp`
  - VoIP-Topic `com.jorisconrad.housephone.watchkitapp.voip`
  - Team `T9CA6D7T8N`
- **Netzwerk auf der Watch:** Außerhalb eines CallKit-Anrufs nur HTTPS über URLSession, kein WebSocket (TN3135).
- **Kompatibilität:** Das Protokoll bleibt rückwärtskompatibel. Die iPhone-App v0.1 muss unverändert weiter funktionieren.
