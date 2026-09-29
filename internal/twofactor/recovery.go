package twofactor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"regexp"
	"strings"
)

// RecoveryCodeCount is how many recovery codes are issued at a time.
const RecoveryCodeCount = 10

var recoveryFormat = regexp.MustCompile(`^[a-z2-7]{5}-?[a-z2-7]{5}$`)

// NewRecoveryCodes returns RecoveryCodeCount single-use codes (xxxxx-xxxxx, lowercase base32, no
// ambiguous 0/1/8/9) and their hashes, in the same order. Only the hashes are stored.
func NewRecoveryCodes() (plain []string, hashes []string, err error) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	seen := map[string]bool{}
	for len(plain) < RecoveryCodeCount {
		b := make([]byte, 7) // 56 bits → 12 base32 chars, 10 are used
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		s := strings.ToLower(enc.EncodeToString(b))[:10]
		if seen[s] {
			continue
		}
		seen[s] = true
		code := s[:5] + "-" + s[5:]
		plain = append(plain, code)
		hashes = append(hashes, HashRecoveryCode(code))
	}
	return plain, hashes, nil
}

func normalizeRecovery(code string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(code)), "-", "")
}

// HashRecoveryCode is the stored form: SHA-256 of the code without case, hyphen or surrounding spaces.
func HashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecovery(code)))
	return hex.EncodeToString(sum[:])
}

// IsRecoveryCode tells a recovery code from a 6-digit TOTP code by its format.
func IsRecoveryCode(code string) bool {
	return recoveryFormat.MatchString(strings.ToLower(strings.TrimSpace(code)))
}
