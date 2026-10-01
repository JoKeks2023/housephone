package dashboard

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// lang is "de" or "en", picked from Accept-Language.
type lang string

func langFor(r *http.Request) lang {
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		switch {
		case strings.HasPrefix(tag, "de"):
			return "de"
		case strings.HasPrefix(tag, "en"):
			return "en"
		}
	}
	return "en"
}

// T returns the text for key; unknown keys come back as they are.
func (l lang) T(key string) string {
	if t, ok := texts[key]; ok {
		if l == "de" {
			return t[0]
		}
		return t[1]
	}
	return key
}

func (l lang) since(d time.Duration) string {
	switch {
	case d < time.Minute:
		return l.T("justNow")
	case d < time.Hour:
		return fmt.Sprintf(l.T("minutes"), int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf(l.T("hours"), int(d.Hours()))
	}
	return fmt.Sprintf(l.T("days"), int(d.Hours()/24))
}

// texts: key → {German, English}.
var texts = map[string][2]string{
	"title":          {"Housephone Bridge", "Housephone Bridge"},
	"status":         {"Status", "Status"},
	"sip":            {"FRITZ!Box", "FRITZ!Box"},
	"registered":     {"Angemeldet", "Registered"},
	"notRegistered":  {"Nicht angemeldet", "Not registered"},
	"publicIP":       {"Öffentliche IP", "Public IP"},
	"unknown":        {"unbekannt", "unknown"},
	"source":         {"Quelle", "Source"},
	"listeners":      {"Zugänge", "Listeners"},
	"public":         {"Tunnel", "Tunnel"},
	"private":        {"Heimnetz", "Home network"},
	"off":            {"aus", "off"},
	"push":           {"Push (APNs)", "Push (APNs)"},
	"configured":     {"Eingerichtet", "Configured"},
	"notConfigured":  {"Nicht eingerichtet", "Not configured"},
	"devicesOnline":  {"Geräte online", "Devices online"},
	"of":             {"von", "of"},
	"fingerprint":    {"Fingerabdruck", "Fingerprint"},
	"version":        {"Version", "Version"},
	"running":        {"Laufzeit", "Uptime"},
	"calls":          {"Aktive Anrufe", "Active calls"},
	"noCalls":        {"Gerade kein Anruf.", "No calls right now."},
	"incoming":       {"Eingehend", "Incoming"},
	"outgoing":       {"Ausgehend", "Outgoing"},
	"connected":      {"verbunden", "connected"},
	"ringing":        {"klingelt", "ringing"},
	"devices":        {"Geräte", "Devices"},
	"profiles":       {"Profile", "Profiles"},
	"profile":        {"Profil", "Profile"},
	"profileHint":    {"Jedes Profil ist ein eigenes IP-Telefon an der FRITZ!Box mit eigener Nummer. Geräte sehen nur die Anrufe, die Anrufliste und die Telefonbücher ihres Profils.", "Each profile is its own IP phone at the FRITZ!Box with its own number. Devices only see their profile's calls, call list and phonebooks."},
	"noNumbers":      {"keine eigene Nummer – keine Anrufliste", "no own number – no call list"},
	"allPhonebooks":  {"alle Telefonbücher", "all phonebooks"},
	"phonebooks":     {"Telefonbücher", "Phonebooks"},
	"move":           {"Verschieben", "Move"},
	"moveTo":         {"In Profil verschieben", "Move to profile"},
	"unknownProfile": {"Dieses Profil gibt es nicht.", "This profile does not exist."},
	"noDevices":      {"Noch kein Gerät gekoppelt.", "No device paired yet."},
	"online":         {"online", "online"},
	"offline":        {"offline", "offline"},
	"lastSeen":       {"zuletzt", "last seen"},
	"never":          {"nie", "never"},
	"via":            {"über", "via"},
	"rename":         {"Umbenennen", "Rename"},
	"save":           {"Sichern", "Save"},
	"remove":         {"Entfernen", "Remove"},
	"lanTitle":       {"Kopplungsanfragen aus dem Heimnetz", "Pairing requests from the home network"},
	"lanHint":        {"Gib ein Gerät nur frei, wenn es genau diesen Code zeigt. Sonst ablehnen.", "Approve a device only if it shows exactly this code. Otherwise deny."},
	"lanApprove":     {"Code stimmt – freigeben", "Code matches – approve"},
	"lanDeny":        {"Ablehnen", "Deny"},
	"lanUntil":       {"wartet bis", "waits until"},
	"lanGone":        {"Anfrage nicht mehr offen", "Request no longer open"},
	"lanGoneDetail":  {"Die Anfrage ist abgelaufen oder wurde schon entschieden. Starte die Kopplung auf dem iPhone neu.", "The request expired or was already decided. Start pairing again on the iPhone."},
	"pairNew":        {"Neues Gerät koppeln", "Pair a new device"},
	"pairHint":       {"Im Heim-WLAN findet Housephone die Bridge von selbst – die Anfrage erscheint dann hier. Alternativ einen einmaligen Code mit QR erzeugen (auch für Tailscale).", "On the home Wi-Fi Housephone finds the bridge by itself – the request then shows up here. Or create a one-time code with a QR code (also for Tailscale)."},
	"deviceName":     {"Name (optional)", "Name (optional)"},
	"createCode":     {"Code erzeugen", "Create code"},
	"pairTitle":      {"Gerät koppeln", "Pair a device"},
	"pairScan":       {"Öffne Housephone auf dem iPhone und scanne diesen Code. Oder tippe den Code ein.", "Open Housephone on the iPhone and scan this code, or type the code in."},
	"code":           {"Code", "Code"},
	"validUntil":     {"Gültig bis", "Valid until"},
	"waiting":        {"Warte auf das Gerät …", "Waiting for the device …"},
	"paired":         {"Gekoppelt", "Paired"},
	"pairedCheck":    {"Warst du das nicht? Entferne das Gerät sofort.", "Wasn't you? Remove the device right away."},
	"expired":        {"Der Code ist abgelaufen. Es wurde kein Gerät gekoppelt.", "The code expired. No device was paired."},
	"cancel":         {"Abbrechen", "Cancel"},
	"back":           {"Zurück", "Back"},
	"removeTitle":    {"Gerät entfernen?", "Remove device?"},
	"removeHint":     {"Das Gerät wird sofort getrennt, laufende Anrufe enden. Zum erneuten Verbinden muss es neu gekoppelt werden.", "The device is disconnected right away and its calls end. It has to pair again to reconnect."},
	"companions":     {"Über dieses Gerät gekoppelt (werden mit entfernt):", "Paired through this device (removed as well):"},
	"keepCompanions": {"Diese Geräte behalten", "Keep these devices"},
	"notFound":       {"Nicht gefunden", "Not found"},
	"failed":         {"Das hat nicht geklappt. Details stehen im Log des Add-ons.", "That didn't work. The add-on log has details."},
	"csrf":           {"Die Seite ist veraltet. Lade sie neu und versuche es noch einmal.", "This page is outdated. Reload it and try again."},
	"nameMissing":    {"Der Name fehlt.", "The name is missing."},
	"justNow":        {"gerade eben", "just now"},
	"minutes":        {"%d min", "%d min"},
	"hours":          {"%d h", "%d h"},
	"days":           {"%d Tage", "%d days"},
}
