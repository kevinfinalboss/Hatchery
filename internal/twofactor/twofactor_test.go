package twofactor

import (
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func newKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.Encrypt("JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, "JBSWY3DPEHPK3PXP") {
		t.Fatal("ciphertext contains the plaintext")
	}
	got, err := c.Decrypt(enc)
	if err != nil || got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
	again, _ := c.Encrypt("JBSWY3DPEHPK3PXP")
	if again == enc {
		t.Fatal("two encryptions of the same secret must differ (random nonce)")
	}
}

func TestCipherRejectsBadKeysAndTampering(t *testing.T) {
	if _, err := NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 31))); err == nil {
		t.Fatal("31-byte key accepted")
	}
	if _, err := NewCipher("not base64!"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	a, _ := NewCipher(newKey(t))
	b, _ := NewCipher(newKey(t))
	enc, _ := a.Encrypt("secret")
	if _, err := b.Decrypt(enc); err == nil {
		t.Fatal("wrong key decrypted")
	}
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 1
	if _, err := a.Decrypt(base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
	if _, err := a.Decrypt("AAAA"); err == nil {
		t.Fatal("short ciphertext decrypted")
	}
}

func code(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	c, err := totp.GenerateCode(secret, at)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestVerifyWindowAndStep(t *testing.T) {
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Z2-7]{32}$`).MatchString(secret) {
		t.Fatalf("secret %q is not 20 bytes of unpadded base32", secret)
	}
	now := time.Unix(1_800_000_015, 0)
	step := now.Unix() / 30
	for _, d := range []time.Duration{0, -30 * time.Second, 30 * time.Second} {
		got, ok := Verify(secret, code(t, secret, now.Add(d)), now)
		if !ok || got != step+int64(d/(30*time.Second)) {
			t.Errorf("offset %v: step %d ok %v, want %d", d, got, ok, step+int64(d/(30*time.Second)))
		}
	}
	for _, d := range []time.Duration{-90 * time.Second, 90 * time.Second} {
		if _, ok := Verify(secret, code(t, secret, now.Add(d)), now); ok {
			t.Errorf("offset %v accepted", d)
		}
	}
	if _, ok := Verify(secret, "12345", now); ok {
		t.Error("5-digit code accepted")
	}
	if _, ok := Verify(secret, " "+code(t, secret, now)+" ", now); !ok {
		t.Error("surrounding spaces should be ignored")
	}
}

func TestOTPAuthURL(t *testing.T) {
	u := OTPAuthURL("kevin", "JBSWY3DPEHPK3PXP")
	for _, want := range []string{"otpauth://totp/Hatchery:kevin?", "secret=JBSWY3DPEHPK3PXP", "issuer=Hatchery"} {
		if !strings.Contains(u, want) {
			t.Errorf("%s: missing %q", u, want)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	plain, hashes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 10 || len(hashes) != 10 {
		t.Fatalf("got %d codes, %d hashes", len(plain), len(hashes))
	}
	seen := map[string]bool{}
	for i, c := range plain {
		if !IsRecoveryCode(c) || !regexp.MustCompile(`^[a-z2-7]{5}-[a-z2-7]{5}$`).MatchString(c) {
			t.Errorf("code %q has the wrong format", c)
		}
		if seen[c] {
			t.Errorf("duplicate code %q", c)
		}
		seen[c] = true
		if HashRecoveryCode(c) != hashes[i] {
			t.Errorf("hash %d does not match its code", i)
		}
	}
	if HashRecoveryCode("ABCDE-23456") != HashRecoveryCode("abcde23456") || HashRecoveryCode(" abcde-23456 ") != HashRecoveryCode("abcde23456") {
		t.Error("recovery codes must compare ignoring case, hyphen and spaces")
	}
	if IsRecoveryCode("123456") || !IsRecoveryCode("ABCDE23456") {
		t.Error("IsRecoveryCode tells a TOTP code from a recovery code by format")
	}
}
