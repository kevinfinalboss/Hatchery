package modsource

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"strings"
	"testing"
)

func TestHashes(t *testing.T) {
	content := "hello mod\n"
	h, n, err := Hashes(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	s1 := sha1.Sum([]byte(content))
	s5 := sha512.Sum512([]byte(content))
	if n != int64(len(content)) || h.SHA1 != hex.EncodeToString(s1[:]) || h.SHA512 != hex.EncodeToString(s5[:]) {
		t.Fatalf("got %+v n=%d", h, n)
	}
}

func TestFingerprintIgnoresWhitespace(t *testing.T) {
	a, _, _ := Hashes(strings.NewReader("a b\tc\r\nd"))
	b, _, _ := Hashes(strings.NewReader("abcd"))
	if a.Fingerprint != b.Fingerprint {
		t.Fatalf("whitespace changed the fingerprint: %d vs %d", a.Fingerprint, b.Fingerprint)
	}
	c, _, _ := Hashes(strings.NewReader("abce"))
	if a.Fingerprint == c.Fingerprint {
		t.Fatal("different content, same fingerprint")
	}
}

func TestMurmur2Vectors(t *testing.T) {
	if got := murmur2(nil, 1); got != 0x5bd15e36 {
		t.Errorf("murmur2(empty, 1) = %#x", got)
	}
}
