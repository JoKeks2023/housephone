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
	"noDevices":      {"Noch kein Gerät gekoppelt.", "No device paired yet."},
	"online":         {"online", "online"},
	"offline":        {"offline", "offline"},
	"lastSeen":       {"zuletzt", "last seen"},
	"never":          {"nie", "never"},
	"via":            {"über", "via"},
	"rename":         {"Umbenennen", "Rename"},
	"save":           {"Sichern", "Save"},
	"remove":         {"Entfernen", "Remove"},
	"pairNew":        {"Neues Gerät koppeln", "Pair a new device"},
	"pairHint":       {"Erzeugt einen einmaligen Code. Das iPhone muss dafür im Heim-WLAN oder per Tailscale verbunden sein.", "Creates a one-time code. The iPhone must be on the home Wi-Fi or on Tailscale."},
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
