package logsafe

import "testing"

func TestNumber(t *testing.T) {
	SetShowNumbers(false)
	for in, want := range map[string]string{
		"":           "",
		"0301234567": "…567",
		"+491701234": "…234",
		"**9":        "…",
		"12":         "…",
	} {
		if got := Number(in); got != want {
			t.Errorf("Number(%q) = %q, want %q", in, got, want)
		}
	}
	SetShowNumbers(true)
	defer SetShowNumbers(false)
	if got := Number("0301234567"); got != "0301234567" {
		t.Fatalf("showNumbers: %q", got)
	}
}

func TestCallerName(t *testing.T) {
	SetShowNumbers(false)
	if a := CallerName("Oma"); a.Key != "hasCallerName" || !a.Value.Bool() {
		t.Fatalf("masked attr %v", a)
	}
	SetShowNumbers(true)
	defer SetShowNumbers(false)
	if a := CallerName("Oma"); a.Key != "callerName" || a.Value.String() != "Oma" {
		t.Fatalf("shown attr %v", a)
	}
}
