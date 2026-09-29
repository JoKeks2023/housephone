package auth

import "testing"

func TestSecretHashRoundTrip(t *testing.T) {
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 43 {
		t.Fatalf("secret length %d, want 43", len(secret))
	}
	hash := HashSecret(secret)
	if !VerifySecret(secret, hash) {
		t.Fatal("secret does not verify against its hash")
	}
	if VerifySecret(secret+"x", hash) {
		t.Fatal("wrong secret verified")
	}
	if VerifySecret(secret, "nothex") || VerifySecret(secret, "abcd") {
		t.Fatal("malformed hash verified")
	}
}

func TestParseBearer(t *testing.T) {
	const id = "9b1d4c2a-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	const secret = "q0x8Zk3VtR2mN7pL4sW9yB6cF1hJ5dG8eA0uI3oK2nM"
	cases := []struct {
		header string
		ok     bool
	}{
		{"Bearer " + id + "." + secret, true},
		{"bearer " + id + "." + secret, true},
		{"Bearer " + id, false},
		{"Bearer " + id + ".", false},
		{"Basic " + id + "." + secret, false},
		{"Bearer 9B1D4C2A-5E6F-4A7B-8C9D-0E1F2A3B4C5D." + secret, false},
		{"Bearer not-a-uuid." + secret, false},
		{"Bearer " + id + ".bad secret!", false},
		{"", false},
	}
	for _, c := range cases {
		gotID, gotSecret, ok := ParseBearer(c.header)
		if ok != c.ok {
			t.Errorf("ParseBearer(%q) ok=%v, want %v", c.header, ok, c.ok)
			continue
		}
		if ok && (gotID != id || gotSecret != secret) {
			t.Errorf("ParseBearer(%q) = %q, %q", c.header, gotID, gotSecret)
		}
	}
}
