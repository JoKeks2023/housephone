// Package profile describes the people a bridge serves (ADR-0008). A
// profile is one IP phone at the FRITZ!Box with its own SIP account and
// landline number; every paired device belongs to exactly one profile and
// only sees that profile's calls, call list and phonebooks.
package profile

import (
	"regexp"
	"strings"
)

// DefaultID is the profile of the sip block in config.yaml. Devices paired
// before profiles existed have no profile stored and belong to it.
const DefaultID = "default"

// MaxProfiles bounds the profiles of one bridge (each one is a SIP
// registration and a UDP port).
const MaxProfiles = 8

// IDPattern is the allowed form of a profile ID.
var IDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Profile is one person (or line) of the household.
type Profile struct {
	ID   string
	Name string
	// SIPUser is the IP phone's user name at the FRITZ!Box.
	SIPUser string
	// Numbers are the profile's own landline numbers. With several
	// profiles they decide which entries of the FRITZ!Box call list a
	// device sees.
	Numbers []string
	// Phonebooks are FRITZ!Box phonebook IDs ("0", "1", …). Empty: all.
	Phonebooks []string
}

// Number is the first own number, shown in the app; "" if none is set.
func (p Profile) Number() string {
	if len(p.Numbers) == 0 {
		return ""
	}
	return p.Numbers[0]
}

// Set is the profiles of a bridge, the default profile first. The zero
// value is a single default profile, as before profiles existed.
type Set struct {
	list []Profile
}

// NewSet builds a set; the first profile must be the default profile.
func NewSet(list ...Profile) Set {
	return Set{list: append([]Profile(nil), list...)}
}

// List returns all profiles, the default profile first.
func (s Set) List() []Profile {
	if len(s.list) == 0 {
		return []Profile{{ID: DefaultID}}
	}
	return append([]Profile(nil), s.list...)
}

// Multi reports whether the household has more than one profile. Only
// then is the call list filtered by own numbers.
func (s Set) Multi() bool { return len(s.list) > 1 }

// Get returns the profile with the given ID ("" means the default).
func (s Set) Get(id string) (Profile, bool) {
	id = Normalize(id)
	for _, p := range s.List() {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

// Normalize maps the stored profile of a device to its ID: devices paired
// before profiles existed store none and belong to the default profile.
func Normalize(id string) string {
	if id == "" {
		return DefaultID
	}
	return id
}

// HistoryAllowed reports whether p may see the FRITZ!Box call list. With
// several profiles a profile without own numbers sees none: its entries
// cannot be told apart from the others'.
func (s Set) HistoryAllowed(p Profile) bool {
	return !s.Multi() || len(p.Numbers) > 0
}

// OwnsNumber reports whether own (the profile's side of a call list entry,
// as the FRITZ!Box writes it) is one of numbers. The FRITZ!Box writes own
// numbers with or without area code, "SIP: " or a country prefix, so only
// the digits without leading zeros are compared, and one may be a suffix of
// the other if the shorter still has at least 5 digits.
func OwnsNumber(own string, numbers []string) bool {
	a := digits(own)
	if a == "" {
		return false
	}
	for _, n := range numbers {
		b := digits(n)
		if b == "" {
			continue
		}
		if a == b {
			return true
		}
		short, long := a, b
		if len(short) > len(long) {
			short, long = long, short
		}
		if len(short) >= 5 && strings.HasSuffix(long, short) {
			return true
		}
	}
	return false
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strings.TrimLeft(b.String(), "0")
}
