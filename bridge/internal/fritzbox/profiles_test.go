package fritzbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JoKeks2023/housephone/bridge/internal/protocol"
)

// ADR-0008: per-profile phonebooks, call list and caller names.

func twoBooks() []protocol.Contact {
	return []protocol.Contact{
		{ID: "0-1", Name: "Kontakt A", Numbers: []protocol.ContactNumber{{Number: "030111111", Type: "home"}}},
		{ID: "1-1", Name: "Kontakt B", Numbers: []protocol.ContactNumber{{Number: "030222222", Type: "home"}}},
	}
}

func TestPhonebookPerProfile(t *testing.T) {
	src := &countingSource{contacts: twoBooks()}
	dir := NewDirectory(DirectoryConfig{Source: src, CountryCode: "49", Logger: discard()})

	all, allTag, err := dir.Phonebook(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	only, onlyTag, err := dir.Phonebook(context.Background(), []string{"1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(all), "Kontakt A") || !strings.Contains(string(all), "Kontakt B") {
		t.Fatalf("all phonebooks: %s", all)
	}
	if strings.Contains(string(only), "Kontakt A") || !strings.Contains(string(only), "Kontakt B") || onlyTag == allTag {
		t.Fatalf("phonebook 1 only: %s (etag %s vs %s)", only, onlyTag, allTag)
	}
	if src.phonebookHits.Load() != 1 {
		t.Fatalf("per-profile phonebooks must share one fetch, got %d", src.phonebookHits.Load())
	}

	if got := dir.CallerNameIn("+4930222222", []string{"0"}); got != "" {
		t.Fatalf("named from another profile's phonebook: %q", got)
	}
	if got := dir.CallerNameIn("+4930222222", []string{"1"}); got != "Kontakt B" {
		t.Fatalf("own phonebook: %q", got)
	}
	if got := dir.CallerNameIn("+4930111111", nil); got != "Kontakt A" {
		t.Fatalf("all phonebooks: %q", got)
	}
}

func TestHistoryPerProfile(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	src := &countingSource{calls: []protocol.HistoryCall{
		{ID: "1", Direction: protocol.HistoryIncoming, Number: "030999", OwnNumber: "1234567", StartedAt: at},
		{ID: "2", Direction: protocol.HistoryOutgoing, Number: "030998", OwnNumber: "SIP: 1234568", StartedAt: at},
		{ID: "3", Direction: protocol.HistoryIncoming, Number: "030997", OwnNumber: "", StartedAt: at},
	}}
	dir := NewDirectory(DirectoryConfig{Source: src, Logger: discard()})

	ids := func(own []string) []string {
		body, err := dir.History(context.Background(), 100, own)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "1234567") || strings.Contains(string(body), "OwnNumber") {
			t.Fatalf("own number leaked into the answer: %s", body)
		}
		var h protocol.History
		if err := json.Unmarshal(body, &h); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range h.Calls {
			out = append(out, c.ID)
		}
		return out
	}
	if got := ids(nil); strings.Join(got, ",") != "1,2,3" {
		t.Fatalf("unfiltered: %v", got)
	}
	if got := ids([]string{"030 1234567"}); strings.Join(got, ",") != "1" {
		t.Fatalf("profile A: %v", got)
	}
	if got := ids([]string{"030 1234568"}); strings.Join(got, ",") != "2" {
		t.Fatalf("profile B: %v", got)
	}
}

func TestCallListOwnNumber(t *testing.T) {
	data := []byte(`<root>
<Call><Id>1</Id><Type>1</Type><Caller>030999</Caller><Called>1234567</Called><Date>01.09.26 10:00</Date><Duration>0:01</Duration></Call>
<Call><Id>2</Id><Type>3</Type><Caller>SIP: 1234568</Caller><Called>030998</Called><Date>01.09.26 10:01</Date><Duration>0:01</Duration></Call>
</root>`)
	calls, err := parseCallList(data, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	own := map[string]string{}
	for _, c := range calls {
		own[c.ID] = c.OwnNumber
	}
	if own["1"] != "1234567" || own["2"] != "SIP: 1234568" {
		t.Fatalf("own numbers %v", own)
	}
}
