# Rahmen

- Durchsetzung nur auf der Bridge; die App vertraut sich selbst nichts an.
- Rückwärtskompatibel: alte `config.yaml`, alte `devices.json`, ältere Apps (ignorieren `welcome.profile`).
- Keine echten Telefonnummern oder Namen in Code, Tests, Fixtures und Doku (fiktiv: 030 1234567 ff., „Profil A/B“).
- Nummern im Log weiter gekürzt.
- Kein Simulator, kein lokaler Xcode-Build; die App baut die CI.
- Keine Huly-Links im Repo.
