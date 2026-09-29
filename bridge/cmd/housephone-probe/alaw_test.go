package main

import "testing"

// sunLinearToALaw is the reference encoder from Sun's public-domain g711.c.
func sunLinearToALaw(pcm int16) byte {
	segEnd := [8]int{0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF, 0x1FFF, 0x3FFF, 0x7FFF}
	v := int(pcm) >> 3
	var mask int
	if v >= 0 {
		mask = 0xD5
	} else {
		mask = 0x55
		v = -v - 1
	}
	seg := 8
	for i, end := range segEnd {
		if v <= end>>3 {
			seg = i
			break
		}
	}
	if seg >= 8 {
		return byte(0x7F ^ mask)
	}
	aval := seg << 4
	if seg < 2 {
		aval |= (v >> 1) & 0x0F
	} else {
		aval |= (v >> seg) & 0x0F
	}
	return byte(aval ^ mask)
}

func TestALawMatchesSunReference(t *testing.T) {
	for v := -32768; v <= 32767; v++ {
		if got, want := alawEncode(int16(v)), sunLinearToALaw(int16(v)); got != want {
			t.Fatalf("alawEncode(%d) = %#02x, want %#02x", v, got, want)
		}
	}
}

func TestALawRoundTripIsClose(t *testing.T) {
	for v := -32768; v <= 32767; v += 7 {
		back := int(alawDecode(alawEncode(int16(v))))
		diff := back - v
		if diff < 0 {
			diff = -diff
		}
		limit := 1024 // quantization step of the top segment
		if v > -256 && v < 256 {
			limit = 16
		}
		if diff > limit {
			t.Fatalf("round trip %d → %d (diff %d)", v, back, diff)
		}
	}
	if alawEncode(0) != 0xD5 {
		t.Fatalf("encode(0) = %#02x, want 0xd5", alawEncode(0))
	}
}
