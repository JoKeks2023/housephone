# T-0018 Mac-App

Vorgabe (2026-10-25): Housephone soll auch als Mac-App laufen, damit man am Schreibtisch mit der Festnetznummer telefoniert. Auftrag: auf einem eigenen Branch (`feat/mac-app`) anfangen.

Stand: Es gibt keinen Mac-Code (Telephone ist mit `legacy/` entfernt, HPHN-38). `HousephoneKit` ist plattformneutral und deklariert schon `.macOS(.v15)`. Die Watch-App zeigt den Weg ohne PushKit/CallKit-Pfad der iPhone-App: eine WebSocket-Verbindung zur Bridge, Audio über `CallAudio` (AVAudioEngine, G.711).
