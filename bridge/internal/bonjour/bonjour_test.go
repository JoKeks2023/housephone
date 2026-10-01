package bonjour

import (
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/mdns"
	"github.com/miekg/dns"
)

func TestTXTCarriesOnlyAFingerprintPrefix(t *testing.T) {
	txt := TXT("hCFfOFjXamFxH0X9tnc_pnS3k8vGHJpP5ACqXm6CvZQ")
	if !slices.Contains(txt, "fp=hCFfOFjX") || !slices.Contains(txt, "proto=hp2") {
		t.Fatalf("txt %v", txt)
	}
	for _, kv := range txt {
		if strings.HasPrefix(kv, "fp=") && len(kv) != 3+FingerprintPrefixLength {
			t.Fatalf("fingerprint not shortened: %s", kv)
		}
	}
}

func TestInstanceName(t *testing.T) {
	for in, want := range map[string]string{
		"Zuhause":                "Zuhause",
		"  Haus.Ost \x07":        "HausOst",
		"":                       "Housephone",
		strings.Repeat("ä", 40):  strings.Repeat("ä", 31),
		"Wohnung \\ Erdgeschoss": "Wohnung  Erdgeschoss",
	} {
		if got := InstanceName(in); got != want {
			t.Errorf("InstanceName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAdvertisedIP(t *testing.T) {
	for _, c := range []struct{ listen, lan, want string }{
		{"", "192.168.0.20", "192.168.0.20"},
		{"0.0.0.0", "192.168.0.20", "192.168.0.20"},
		{"::", "192.168.0.20", "192.168.0.20"},
		{"192.168.0.21", "192.168.0.20", "192.168.0.21"},
		{"127.0.0.1", "192.168.0.20", ""},
		{"100.101.102.103", "192.168.0.20", ""}, // Tailscale only
		{"", "", ""},
		{"", "203.0.113.5", ""},
	} {
		got := AdvertisedIP(c.listen, c.lan)
		if (got == nil && c.want != "") || (got != nil && got.String() != c.want) {
			t.Errorf("AdvertisedIP(%q, %q) = %v, want %q", c.listen, c.lan, got, c.want)
		}
	}
}

// The zone answers a browse (PTR) and resolve (SRV, TXT, A) the way
// NWBrowser asks.
func TestZoneAnswersBrowseAndResolve(t *testing.T) {
	ip := net.ParseIP("192.168.0.20")
	zone, err := mdns.NewMDNSService(InstanceName("Zuhause"), Service, "local.", "housephone-192-168-0-20.local.", 8081, []net.IP{ip}, TXT("hCFfOFjXamFxH0X9"))
	if err != nil {
		t.Fatal(err)
	}
	ptr := zone.Records(dns.Question{Name: Service + ".local.", Qtype: dns.TypePTR, Qclass: dns.ClassINET})
	var instance string
	for _, rr := range ptr {
		if p, ok := rr.(*dns.PTR); ok {
			instance = p.Ptr
		}
	}
	if instance != "Zuhause."+Service+".local." {
		t.Fatalf("PTR %v", ptr)
	}
	all := zone.Records(dns.Question{Name: instance, Qtype: dns.TypeANY, Qclass: dns.ClassINET})
	var port uint16
	var txt []string
	for _, rr := range all {
		switch r := rr.(type) {
		case *dns.SRV:
			port = r.Port
		case *dns.TXT:
			txt = r.Txt
		}
	}
	if port != 8081 || !slices.Contains(txt, "pair=lan") {
		t.Fatalf("SRV/TXT %v", all)
	}
}

func TestStartRefusesWithoutHomeAddress(t *testing.T) {
	if _, err := Start(Config{Name: "x", Port: 8081}); err == nil {
		t.Fatal("started without an address")
	}
	if _, err := Start(Config{Name: "x", IP: net.ParseIP("127.0.0.1"), Port: 8081}); err == nil {
		t.Fatal("started on loopback")
	}
}
