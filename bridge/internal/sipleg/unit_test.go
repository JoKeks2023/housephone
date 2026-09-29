package sipleg

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"

	"github.com/JoKeks2023/housephone/bridge/internal/codec"
)

const fritzOffer = "v=0\r\n" +
	"o=user 123 456 IN IP4 192.168.178.1\r\n" +
	"s=call\r\n" +
	"c=IN IP4 192.168.178.1\r\n" +
	"t=0 0\r\n" +
	"m=audio 7078 RTP/AVP 8 0 9 18 101\r\n" +
	"a=rtpmap:8 PCMA/8000\r\n" +
	"a=rtpmap:0 PCMU/8000\r\n" +
	"a=rtpmap:9 G722/8000\r\n" +
	"a=rtpmap:18 G729/8000\r\n" +
	"a=rtpmap:101 telephone-event/8000\r\n" +
	"a=fmtp:101 0-15\r\n" +
	"a=sendrecv\r\n"

func TestChooseFromOfferPrefersG722(t *testing.T) {
	choice, err := chooseFromOffer([]byte(fritzOffer))
	if err != nil {
		t.Fatal(err)
	}
	if choice.codec != codec.G722 || choice.offered[codec.G722] != codecG722 {
		t.Fatalf("chose %v %+v", choice.codec, choice.offered)
	}
	if choice.telephoneEvent == nil || choice.telephoneEvent.PayloadType != 101 {
		t.Fatalf("telephone-event not found: %+v", choice.telephoneEvent)
	}
	if got := choice.diagoCodecs(codec.G722); len(got) != 2 || got[0] != codecG722 {
		t.Fatalf("answer codecs %+v", got)
	}
	// The watch answers the same INVITE with PCMA (v1.1).
	if !choice.has(codec.PCMA) || !choice.has(codec.PCMU) {
		t.Fatalf("offered codecs %+v", choice.offered)
	}
	if got := choice.diagoCodecs(codec.PCMA); got[0].Name != "PCMA" || got[0].PayloadType != 8 {
		t.Fatalf("PCMA answer codecs %+v", got)
	}
}

func TestChooseFromOfferFallsBackAndKeepsPayloadTypes(t *testing.T) {
	offer := strings.NewReplacer("m=audio 7078 RTP/AVP 8 0 9 18 101", "m=audio 7078 RTP/AVP 0 96", "a=rtpmap:101 telephone-event/8000", "a=rtpmap:96 telephone-event/8000").Replace(fritzOffer)
	choice, err := chooseFromOffer([]byte(offer))
	if err != nil {
		t.Fatal(err)
	}
	if choice.codec != codec.PCMU || choice.telephoneEvent == nil || choice.telephoneEvent.PayloadType != 96 {
		t.Fatalf("got %+v te=%+v", choice.codec, choice.telephoneEvent)
	}
}

func TestChooseFromOfferRejectsUnsupported(t *testing.T) {
	offer := strings.Replace(fritzOffer, "m=audio 7078 RTP/AVP 8 0 9 18 101", "m=audio 7078 RTP/AVP 18", 1)
	if _, err := chooseFromOffer([]byte(offer)); err == nil {
		t.Fatal("expected error for G.729-only offer")
	}
	if _, err := chooseFromOffer(nil); err == nil {
		t.Fatal("expected error without SDP")
	}
}

func TestOfferSDPIsNarrowed(t *testing.T) {
	sdp := string(offerSDP(codec.PCMA))
	if !strings.Contains(sdp, "m=audio 9 RTP/AVP 8 101") || strings.Contains(sdp, "G722") {
		t.Fatalf("unexpected SDP:\n%s", sdp)
	}
	choice, err := chooseFromOffer([]byte(sdp))
	if err != nil || choice.codec != codec.PCMA {
		t.Fatalf("own SDP does not parse back: %v %v", choice.codec, err)
	}
}

func TestDTMFPackets(t *testing.T) {
	for key, want := range map[rune]uint8{'0': 0, '9': 9, '*': 10, '#': 11} {
		got, err := dtmfEvent(key)
		if err != nil || got != want {
			t.Errorf("dtmfEvent(%q) = %d, %v", key, got, err)
		}
	}
	if _, err := dtmfEvent('A'); err == nil {
		t.Error("A must be rejected")
	}

	pkts := dtmfPackets(5, 101, 16000)
	if len(pkts) != 6 { // 3 progress (50/100/150 ms) + 3 end
		t.Fatalf("got %d packets", len(pkts))
	}
	if !pkts[0].Marker || pkts[1].Marker {
		t.Fatal("marker only on the first packet")
	}
	var lastDuration uint16
	for i, p := range pkts {
		if p.PayloadType != 101 || p.Timestamp != 16000 || p.Payload[0] != 5 {
			t.Fatalf("packet %d header/event wrong: %+v %v", i, p.Header, p.Payload)
		}
		end := p.Payload[1]&0x80 != 0
		duration := binary.BigEndian.Uint16(p.Payload[2:])
		if duration < lastDuration {
			t.Fatalf("duration decreased at %d", i)
		}
		lastDuration = duration
		if end != (i >= 3) {
			t.Fatalf("end bit wrong at %d", i)
		}
	}
	if lastDuration != 1280 {
		t.Fatalf("final duration %d, want 1280 (160 ms)", lastDuration)
	}
}

func TestAnsweredElsewhere(t *testing.T) {
	req := sip.NewRequest(sip.CANCEL, sip.Uri{Host: "x"})
	if answeredElsewhere(req) {
		t.Fatal("no Reason header")
	}
	req.AppendHeader(sip.NewHeader("Reason", `SIP;cause=200;text="Call completed elsewhere"`))
	if !answeredElsewhere(req) {
		t.Fatal("cause=200 not detected")
	}
	other := sip.NewRequest(sip.CANCEL, sip.Uri{Host: "x"})
	other.AppendHeader(sip.NewHeader("Reason", `Q.850;cause=16`))
	if answeredElsewhere(other) {
		t.Fatal("Q.850 reason misdetected")
	}
}

func TestAnonymousCaller(t *testing.T) {
	for _, s := range []string{"anonymous", "Anonymous", "unknown"} {
		if !isAnonymous(s) {
			t.Errorf("%q should be anonymous", s)
		}
	}
	if isAnonymous("030123") {
		t.Error("number flagged as anonymous")
	}
}
