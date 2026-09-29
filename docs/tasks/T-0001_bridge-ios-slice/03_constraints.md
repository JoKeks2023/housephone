# Randbedingungen

- **Lokaler Mac:** M1 mit 8 GB RAM
  - kein Simulator
  - keine schweren Xcode-Builds, die App baut die CI (GitHub Actions, macOS-Runner)
  - lokal nur `go test`, `swift test` (Kit) und `swiftc -typecheck`
- **Keine Änderungen am alten macOS-Code** (`Telephone/`, `Domain/`, `UseCases/` …). Er bleibt als Referenz.
- **Git:** Feature-Branches, PRs gegen `master`, Merge-Commit (kein Squash).
- **Identitäten:**
  - Bundle-ID `com.jorisconrad.housephone`
  - Team `T9CA6D7T8N`
  - VoIP-Topic `com.jorisconrad.housephone.voip`
- **Secrets gehören nicht ins Repo:** APNs-Key, SIP-Passwort und Cloudflare-Token kommen per Datei oder Umgebungsvariable.
- **Lizenzen:** Neue Abhängigkeiten nur mit permissiver Lizenz (MIT/BSD/Apache/MPL).
