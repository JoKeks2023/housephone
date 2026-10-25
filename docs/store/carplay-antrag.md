# CarPlay-Entitlement beantragen

Die CarPlay-Oberfläche ist gebaut (PR #51, `ios/Housephone/CarPlay/`), aber aus, bis Apple das Entitlement `com.apple.developer.carplay-communication` für die Bundle-ID freigibt.

## Antrag stellen

1. Anmelden bei developer.apple.com → *Contact Us → Request CarPlay entitlement* (Seite „CarPlay Entitlement Request“, mit dem Account-Holder).
2. Kategorie **Communication** (Anruf- und Nachrichten-Apps).
3. Bundle-ID der veröffentlichten App angeben.
4. Text unten einfügen (Englisch, Apple liest Englisch).

## Antworttext

> Housephone is a VoIP calling app that lets users place and receive calls on their own home landline number (via their own FRITZ!Box and a self-hosted bridge) from iPhone and Apple Watch. All calls use CallKit.
>
> CarPlay use: a CPTabBarTemplate with three CPListTemplates (Favorites, Recents, Contacts) for starting calls with one tap. Incoming calls are presented by the system through CallKit. There is no text input, no media playback, and no content other than the user's own favorites, call list and contacts. We use only the templates available to Communication apps.
>
> We request the Communication entitlement (com.apple.developer.carplay-communication).

## Nach der Freigabe

1. Provisioning-Profile neu erzeugen (CarPlay-Entitlement ist enthalten).
2. Einrichtungsskript mit `--carplay` ausführen (setzt `HOUSEPHONE_CARPLAY_SUFFIX` und die Entitlement-Datei).
3. Im CarPlay-Simulator oder Fahrzeug prüfen: Listen laden, Anruf starten, Fehler-Alert.
4. Store-Eintrag um CarPlay ergänzen (Beschreibung, Screenshots) und neues Build einreichen.

Bis dahin CarPlay weder in der Beschreibung noch in Screenshots erwähnen. Die Beschreibung in `listing.md` tut das nur in den Stichwörtern, nicht im Text.
