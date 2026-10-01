# Scope

**Drin**
- FRITZ!Box finden: `fritz.box` auflösen, dann die Gateways des WLANs, dann `192.168.178.1`; bestätigen über `tr64desc.xml` (ohne Anmeldung). Nur private IPv4-Adressen.
- IP-Telefon per TR-064 anlegen (`X_VoIP`): Nummern und vorhandene IP-Telefone lesen, freien Platz und Benutzernamen wählen, zufälliges Kennwort, `X_AVM-DE_SetClient4`.
- Vorhandenes IP-Telefon dieser App wiedererkennen (Client-ID oder Name) und wahlweise weiterverwenden (neues Kennwort) oder neu anlegen.
- Zwei-Faktor-Bestätigung der FRITZ!Box (`X_AVM-DE_Auth`) mit Hinweis „Bestätige an der FRITZ!Box“.
- Eine Anmeldung für alles: dieselbe schaltet Telefonbuch und Anrufliste ein.
- Manuelle Einrichtung bleibt als „Manuell“.
- #33: Ursache des wackelnden Tests beheben.

**Nicht drin**
- SSDP (bräuchte das Multicast-Entitlement).
- Die Bridge legt ihr IP-Telefon selbst an (Folgeaufgabe, siehe Umsetzungsbericht).
- Heim-WLAN-Name automatisch auslesen (bräuchte den Standort).
