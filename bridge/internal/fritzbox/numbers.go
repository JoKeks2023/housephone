package fritzbox

import "strings"

// cleanNumber keeps what is dialable: a leading "+", digits, "*" and "#".
// The FRITZ!Box stores numbers as typed, e.g. "030 / 12 34-56".
func cleanNumber(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9', r == '*', r == '#':
			b.WriteRune(r)
		case r == '+' && b.Len() == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nationalNumber maps international forms of the own country to the
// national form used by the FRITZ!Box, so "+4930123456", "004930123456"
// and "030123456" compare equal. countryCode is e.g. "49".
func nationalNumber(number, countryCode string) string {
	n := cleanNumber(number)
	if countryCode == "" {
		return n
	}
	for _, prefix := range []string{"+" + countryCode, "00" + countryCode} {
		if rest, ok := strings.CutPrefix(n, prefix); ok && rest != "" {
			return "0" + strings.TrimPrefix(rest, "0")
		}
	}
	return n
}
