package auth

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// rfc6238Secret is the SHA-1 key of RFC 6238's appendix B.
var rfc6238Secret = []byte("12345678901234567890")

// The appendix's SHA-1 column, eight digits at each of its moments.
func TestTOTPMatchesTheRFC6238Vectors(t *testing.T) {
	vectors := []struct {
		unix int64
		code string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, v := range vectors {
		step := totpStep(time.Unix(v.unix, 0))
		if got := hotp(rfc6238Secret, uint64(step), 8); got != v.code {
			t.Errorf("at %d: code %s, want %s", v.unix, got, v.code)
		}
		// Six digits are the last six of the eight.
		if got := hotp(rfc6238Secret, uint64(step), TOTPDigits); got != v.code[2:] {
			t.Errorf("at %d: six digit code %s, want %s", v.unix, got, v.code[2:])
		}
	}
}

func TestATOTPCodeIsAcceptedOneStepEitherSideAndNoFurther(t *testing.T) {
	now := time.Unix(1234567890, 0)
	step := totpStep(now)
	codeAt := func(offset int64) string { return hotp(rfc6238Secret, uint64(step+offset), TOTPDigits) }

	for _, offset := range []int64{-1, 0, 1} {
		got, ok := matchTOTP(rfc6238Secret, codeAt(offset), now, 0)
		if !ok || got != step+offset {
			t.Errorf("a code %d steps off was refused or matched the wrong step: %d %v", offset, got, ok)
		}
	}
	for _, offset := range []int64{-2, 2} {
		if _, ok := matchTOTP(rfc6238Secret, codeAt(offset), now, 0); ok {
			t.Errorf("a code %d steps off was accepted", offset)
		}
	}
	if _, ok := matchTOTP(rfc6238Secret, "12345", now, 0); ok {
		t.Error("a five digit code was accepted")
	}
	spaced := codeAt(0)[:3] + " " + codeAt(0)[3:]
	if _, ok := matchTOTP(rfc6238Secret, spaced, now, 0); !ok {
		t.Error("a code typed with a space in the middle was refused")
	}
}

func TestATOTPCodeCannotBeUsedTwice(t *testing.T) {
	now := time.Unix(1234567890, 0)
	code := hotp(rfc6238Secret, uint64(totpStep(now)), TOTPDigits)
	step, ok := matchTOTP(rfc6238Secret, code, now, 0)
	if !ok {
		t.Fatal("the current code was refused")
	}
	if _, ok := matchTOTP(rfc6238Secret, code, now, step); ok {
		t.Fatal("the same code was accepted again")
	}
	// Nor a code from before the one used, though it is still within the skew.
	earlier := hotp(rfc6238Secret, uint64(step-1), TOTPDigits)
	if _, ok := matchTOTP(rfc6238Secret, earlier, now, step); ok {
		t.Fatal("an older code was accepted after a newer one was used")
	}
	// The next step's code is still good.
	later := hotp(rfc6238Secret, uint64(step+1), TOTPDigits)
	if _, ok := matchTOTP(rfc6238Secret, later, now.Add(TOTPPeriod), step); !ok {
		t.Fatal("the next code was refused")
	}
}

func TestTheOtpauthAddressCarriesTheSecretAndIssuer(t *testing.T) {
	uri := otpauthURI(rfc6238Secret, "ada@example.com")
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" {
		t.Fatalf("not a totp address: %s", uri)
	}
	if !strings.HasPrefix(parsed.Path, "/Armature:ada@example.com") {
		t.Fatalf("label is %q", parsed.Path)
	}
	q := parsed.Query()
	if q.Get("secret") != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" || q.Get("issuer") != TOTPIssuer || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Fatalf("query is %v", q)
	}
}

func TestRecoveryCodesAreLongAndForgiveHowTheyAreTyped(t *testing.T) {
	seen := map[string]bool{}
	for range RecoveryCodeCount {
		code, err := newRecoveryCode()
		if err != nil {
			t.Fatal(err)
		}
		if seen[code] {
			t.Fatal("a recovery code repeated")
		}
		seen[code] = true
		if len(normalizeCode(code)) != 16 || strings.Count(code, "-") != 3 {
			t.Fatalf("code %q is not four groups of four", code)
		}
		if isTOTPCode(code) {
			t.Fatalf("code %q reads as an authenticator code", code)
		}
	}
	user := uuid.New()
	if string(recoveryDigest(user, "ABCD-efgh ijkl-mnop")) != string(recoveryDigest(user, "abcdefghijklmnop")) {
		t.Fatal("case, spaces and dashes changed the digest")
	}
	if string(recoveryDigest(user, "abcdefghijklmnop")) == string(recoveryDigest(uuid.New(), "abcdefghijklmnop")) {
		t.Fatal("the same code has the same digest for two people")
	}
	if !isTOTPCode(" 123 456 ") || isTOTPCode("12345a") {
		t.Fatal("isTOTPCode misread a code")
	}
}
