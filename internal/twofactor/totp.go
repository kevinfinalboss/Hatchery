package twofactor

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"net/url"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// Issuer is the account label authenticator apps show.
const Issuer = "Hatchery"

const period = 30

var opts = totp.ValidateOpts{Period: period, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

// NewSecret returns 20 random bytes in unpadded base32, the form authenticator apps expect.
func NewSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// OTPAuthURL is the otpauth:// URI the setup QR code encodes.
func OTPAuthURL(username, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", Issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(Issuer+":"+username) + "?" + v.Encode()
}

// Verify checks a 6-digit code against the secret at now, allowing one 30-second step either way
// for clock drift. It returns the step (unix time / 30) the code belongs to, which the caller
// records so the same code cannot be used twice.
func Verify(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	step := now.Unix() / period
	for _, s := range []int64{step, step - 1, step + 1} {
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*period, 0), opts)
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}
