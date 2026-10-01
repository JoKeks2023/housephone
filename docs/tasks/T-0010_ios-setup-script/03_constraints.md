# Rahmen

- Kein Simulator, kein lokaler Xcode-Build (M1, 8 GB); nur `xcodebuild -list`/`-showBuildSettings` lokal, gebaut wird in der CI
- Auswahl per Liste statt Freitext, Freitext nur über „Andere …“
- Keine Namen oder Nummern aus dem Schlüsselbund im Repo
- `project.pbxproj` nur einmal anfassen; danach bleibt sie für alle gleich
