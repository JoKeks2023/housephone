package codec

import "testing"

func TestPayloadTypes(t *testing.T) {
	if G722.PayloadType() != 9 || PCMA.PayloadType() != 8 || PCMU.PayloadType() != 0 {
		t.Fatal("wrong static payload types")
	}
}

func TestChooseFirst(t *testing.T) {
	cases := []struct {
		offered []Codec
		want    Codec
		ok      bool
	}{
		{[]Codec{PCMU, PCMA, G722}, G722, true},
		{[]Codec{PCMU, PCMA}, PCMA, true},
		{[]Codec{PCMU}, PCMU, true},
		{nil, "", false},
	}
	for _, c := range cases {
		got, ok := ChooseFirst(c.offered)
		if got != c.want || ok != c.ok {
			t.Errorf("ChooseFirst(%v) = %v %v, want %v %v", c.offered, got, ok, c.want, c.ok)
		}
	}
}

func TestParsing(t *testing.T) {
	if c, ok := FromName("g722"); !ok || c != G722 {
		t.Fatal("FromName g722")
	}
	if _, ok := FromName("opus"); ok {
		t.Fatal("opus is not supported")
	}
	if c, ok := FromMimeType("audio/PCMA"); !ok || c != PCMA {
		t.Fatal("FromMimeType audio/PCMA")
	}
	if _, ok := FromMimeType("video/PCMA"); ok {
		t.Fatal("video accepted")
	}
	if G722.MimeType() != "audio/G722" {
		t.Fatal("MimeType")
	}
}
