package profile

import "testing"

func TestOwnsNumber(t *testing.T) {
	numbers := []string{"030 1234567"}
	for own, want := range map[string]bool{
		"0301234567":       true,
		"1234567":          true,
		"SIP: 1234567":     true,
		"+49301234567":     true,
		"004930 1234567":   true,
		"1234568":          false,
		"4567":             false, // too short to compare as a suffix
		"":                 false,
		"0301234567890123": false,
	} {
		if got := OwnsNumber(own, numbers); got != want {
			t.Errorf("OwnsNumber(%q) = %v, want %v", own, got, want)
		}
	}
}

func TestSet(t *testing.T) {
	var zero Set
	if zero.Multi() || len(zero.List()) != 1 || zero.List()[0].ID != DefaultID {
		t.Fatalf("zero set = %+v", zero.List())
	}
	if _, ok := zero.Get(""); !ok {
		t.Fatal("empty ID must resolve to the default profile")
	}
	s := NewSet(Profile{ID: DefaultID}, Profile{ID: "b", Numbers: []string{"0301234568"}})
	if !s.Multi() {
		t.Fatal("two profiles are multi")
	}
	if _, ok := s.Get("c"); ok {
		t.Fatal("unknown profile found")
	}
	a, _ := s.Get(DefaultID)
	b, _ := s.Get("b")
	if s.HistoryAllowed(a) || !s.HistoryAllowed(b) {
		t.Fatal("with several profiles only profiles with numbers see the call list")
	}
	if !zero.HistoryAllowed(Profile{ID: DefaultID}) {
		t.Fatal("a single profile always sees the call list")
	}
}
