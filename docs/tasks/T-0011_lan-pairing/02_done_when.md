# Fertig, wenn

- [x] Ablauf Start → Angebot → Aufdecken → Freigabe/Ablehnung/Ablauf in Go getestet (`internal/signaling/lanpair_test.go`), inkl. Long-Poll, Bindung an den Absender, Grenzen, falsches Commitment, falscher Beweis, öffentlicher Listener `403 home_network_required`
- [x] Krypto-Vektoren aus den Primitiven erzeugt; Go und Swift reproduzieren Commitment, Nachrichten, Transkript, SAS, Schlüssel, Signaturen und Versiegelung
- [x] Swift-Client gegen eine nachgebaute Bridge: Freigabe pinnt den Bridge-Schlüssel, Ablehnung/Ablauf/fremde Bridge/außerhalb des Heimnetzes hinterlassen keinen Schlüssel
- [x] Freigabe in TUI, CLI und Dashboard getestet (Fakes), Dashboard mit CSRF und nur über den Ingress-Proxy
- [x] `go test -race ./...`, `go vet ./...`, `swift test` grün
- [x] CI grün (App-Build mit `BridgeBrowser`/`LanPairingView`)
- [ ] Auf echtem iPhone im Heim-WLAN gegen die echte Bridge gekoppelt (User)
- [ ] Bonjour im HA-Add-on neben dem mDNS von Home Assistant (User)
