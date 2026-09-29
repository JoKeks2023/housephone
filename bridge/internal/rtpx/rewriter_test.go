package rtpx

import (
	"testing"

	"github.com/pion/rtp"
)

func pkt(ssrc uint32, seq uint16, ts uint32) *rtp.Packet {
	return &rtp.Packet{Header: rtp.Header{Version: 2, SSRC: ssrc, SequenceNumber: seq, Timestamp: ts, PayloadType: 9}}
}

func TestRewriterKeepsFirstStream(t *testing.T) {
	r := Rewriter{SSRC: 42, PayloadType: 111, SetPayloadType: true, TimestampStep: 160}
	for i := range 3 {
		p := pkt(7, uint16(100+i), uint32(1000+160*i))
		r.Rewrite(p)
		if p.SSRC != 42 || p.PayloadType != 111 {
			t.Fatalf("header not rewritten: %+v", p.Header)
		}
		if p.SequenceNumber != uint16(100+i) || p.Timestamp != uint32(1000+160*i) {
			t.Fatalf("first stream numbering changed: %+v", p.Header)
		}
	}
}

func TestRewriterKeepsGaps(t *testing.T) {
	r := Rewriter{TimestampStep: 160}
	r.Rewrite(pkt(7, 10, 1600))
	p := pkt(7, 13, 2080) // two packets lost
	r.Rewrite(p)
	if p.SequenceNumber != 13 || p.Timestamp != 2080 {
		t.Fatalf("gap not preserved: %+v", p.Header)
	}
}

func TestRewriterContinuesAcrossSSRCChange(t *testing.T) {
	r := Rewriter{SSRC: 99, TimestampStep: 160}
	r.Rewrite(pkt(1, 65535, 4294967200)) // wraps on the next packet
	next := pkt(2, 500, 123456)
	r.Rewrite(next)
	if next.SequenceNumber != 0 {
		t.Fatalf("sequence not continuous: %d", next.SequenceNumber)
	}
	if next.Timestamp != 64 { // 4294967200 + 160 wraps around
		t.Fatalf("timestamp not continuous: %d", next.Timestamp)
	}
	if !next.Marker {
		t.Fatal("marker not set on stream switch")
	}
	after := pkt(2, 501, 123616)
	r.Rewrite(after)
	if after.SequenceNumber != 1 || after.Timestamp != next.Timestamp+160 || after.SSRC != 99 {
		t.Fatalf("following packet wrong: %+v", after.Header)
	}
}

func TestRewriterReorderedPacketDoesNotMoveBase(t *testing.T) {
	r := Rewriter{TimestampStep: 160}
	r.Rewrite(pkt(1, 10, 1600))
	r.Rewrite(pkt(1, 12, 1920))
	r.Rewrite(pkt(1, 11, 1760)) // late packet
	switched := pkt(2, 0, 0)
	r.Rewrite(switched)
	if switched.SequenceNumber != 13 || switched.Timestamp != 2080 {
		t.Fatalf("switch should follow newest packet: %+v", switched.Header)
	}
}

func TestRewriterStripsExtensions(t *testing.T) {
	r := Rewriter{StripExtensions: true}
	p := pkt(1, 1, 1)
	if err := p.SetExtension(1, []byte{0x10}); err != nil {
		t.Fatal(err)
	}
	p.CSRC = []uint32{5}
	r.Rewrite(p)
	if p.Extension || len(p.Extensions) != 0 || len(p.CSRC) != 0 {
		t.Fatalf("extensions not stripped: %+v", p.Header)
	}
	if _, err := p.Marshal(); err != nil {
		t.Fatal(err)
	}
}

func TestSeqNewer(t *testing.T) {
	if !seqNewer(1, 0) || !seqNewer(0, 65535) || seqNewer(0, 1) || seqNewer(5, 5) {
		t.Fatal("seqNewer wrong")
	}
}
