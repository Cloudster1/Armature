package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Time-based one-time passwords as RFC 6238 has them and every authenticator
// app reads them: HMAC-SHA1, thirty second steps, six digits.
const (
	TOTPDigits = 6
	TOTPPeriod = 30 * time.Second
	// totpSkewSteps is how many steps either side of now a code is accepted
	// from, for a phone whose clock is a little off.
	totpSkewSteps = 1
	// totpSecretBytes is the key length RFC 4226 recommends for SHA-1.
	totpSecretBytes = 20
	// TOTPIssuer is the name an authenticator app files the account under.
	TOTPIssuer = "Armature"

	// RecoveryCodeCount is how many single-use codes a person is given.
	RecoveryCodeCount = 10
	// recoveryCodeBytes is 80 bits, sixteen base32 characters in four groups.
	recoveryCodeBytes = 10
	recoveryGroup     = 4
)

// base32NoPad is how authenticator apps expect a secret to be typed or scanned.
var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// hotp is the code for one counter value, truncated as RFC 4226 section 5.3 says.
func hotp(secret []byte, counter uint64, digits int) string {
	mac := hmac.New(sha1.New, secret)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%mod)
}

// totpStep is the thirty second window a moment falls in.
func totpStep(at time.Time) int64 {
	return at.Unix() / int64(TOTPPeriod/time.Second)
}

// matchTOTP finds the step near now whose code was typed, refusing any step at
// or before last so a code that was already used cannot be used again.
func matchTOTP(secret []byte, code string, now time.Time, last int64) (int64, bool) {
	code = normalizeCode(code)
	if len(code) != TOTPDigits {
		return 0, false
	}
	current := totpStep(now)
	found, ok := int64(0), false
	// Every candidate is computed and compared, so the time taken does not
	// say which step matched.
	for step := current - totpSkewSteps; step <= current+totpSkewSteps; step++ {
		want := hotp(secret, uint64(step), TOTPDigits)
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 && step > last && !ok {
			found, ok = step, true
		}
	}
	return found, ok
}

// newTOTPSecret makes the key an authenticator app is given.
func newTOTPSecret() ([]byte, error) {
	secret := make([]byte, totpSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("make an authenticator secret: %w", err)
	}
	return secret, nil
}

// otpauthURI is what the QR code carries, in the form the apps agree on.
func otpauthURI(secret []byte, account string) string {
	label := url.PathEscape(TOTPIssuer + ":" + account)
	q := url.Values{}
	q.Set("secret", base32NoPad.EncodeToString(secret))
	q.Set("issuer", TOTPIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(TOTPDigits))
	q.Set("period", fmt.Sprint(int(TOTPPeriod/time.Second)))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// newRecoveryCode makes one code to type when the phone is gone.
func newRecoveryCode() (string, error) {
	raw := make([]byte, recoveryCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make a recovery code: %w", err)
	}
	plain := strings.ToLower(base32NoPad.EncodeToString(raw))
	var groups []string
	for i := 0; i < len(plain); i += recoveryGroup {
		groups = append(groups, plain[i:min(i+recoveryGroup, len(plain))])
	}
	return strings.Join(groups, "-"), nil
}

// normalizeCode forgives how a code was typed: case, spaces and dashes.
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ' || r == '\t':
			return -1
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return r
	}, code)
}

// isTOTPCode says whether a typed code is the app's six digits rather than a
// recovery code, which is longer and has letters.
func isTOTPCode(code string) bool {
	code = normalizeCode(code)
	if len(code) != TOTPDigits {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// recoveryDigest is what is kept of a recovery code. The owner's id goes into
// it, so one table of digests cannot be tried against every account at once.
func recoveryDigest(userID uuid.UUID, code string) []byte {
	return HashToken(userID.String() + ":" + normalizeCode(code))
}
