# Randbedingungen

- Keine echten Telefonnummern in Code, Tests, Logs (Fixtures nutzen die bestehenden fiktiven Nummern).
- Relay speichert nichts auf Platte, kein Zugriffslog; Tokens im Log gekürzt.
- Kein lokaler Xcode-Build, kein Simulator (M1, 8 GB); App-Build prüft die CI.
- Ältere Apps ohne `pushKey`: über das Relay kein Push, Selbsttest warnt.
