//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
)

// totpCode is what an authenticator app shows for a secret at a moment. It is
// written apart from the server's own, so the suite does not grade itself.
func totpCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		t.Fatalf("the secret %q is not base32: %v", secret, err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/int64(auth.TOTPPeriod/time.Second)))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

// wrongCode is six digits the app is not showing.
func wrongCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	for _, candidate := range []string{"000000", "111111", "222222"} {
		clash := false
		for _, offset := range []time.Duration{-auth.TOTPPeriod, 0, auth.TOTPPeriod, 2 * auth.TOTPPeriod} {
			if totpCode(t, secret, at.Add(offset)) == candidate {
				clash = true
			}
		}
		if !clash {
			return candidate
		}
	}
	t.Fatal("every candidate was a real code")
	return ""
}

// enrolled is somebody's second factor as a test holds it: the secret, the
// recovery codes, and when the confirming code was read, whose step is spent.
type enrolled struct {
	secret string
	codes  []string
	at     time.Time
}

// next is the code of the step after the one spent, which a sign-in right
// after enrolling may use.
func (e enrolled) next(t *testing.T) string {
	return totpCode(t, e.secret, e.at.Add(auth.TOTPPeriod))
}

// enrol turns two-step sign-in on for the client's account.
func enrol(t *testing.T, c *client) enrolled {
	t.Helper()
	started := obj(t, want(t, c.post("/api/v1/auth/me/second-factor", nil), http.StatusCreated, "start two-step sign-in"), "enrolment")
	secret := started["secret"].(string)
	at := time.Now()
	confirmed := want(t, c.post("/api/v1/auth/me/second-factor/confirm", map[string]string{"code": totpCode(t, secret, at)}), http.StatusOK, "confirm with the app's code")
	var codes []string
	for _, code := range list(t, confirmed, "recoveryCodes") {
		codes = append(codes, code.(string))
	}
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(codes), auth.RecoveryCodeCount)
	}
	return enrolled{secret: secret, codes: codes, at: at}
}

// challengeFor signs in with the password and returns the challenge the
// answer carries instead of a session.
func challengeFor(t *testing.T, c *client, email string) string {
	t.Helper()
	answer := want(t, c.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusAccepted, "a password sign-in asks for the code")
	for _, cookie := range answer.Cookies {
		if cookie.Name == testCookieName && cookie.Value != "" {
			t.Fatal("the password alone set a session cookie")
		}
	}
	challenge, _ := answer.Body["challenge"].(string)
	if challenge == "" {
		t.Fatalf("the answer carries no challenge: %s", answer.Raw)
	}
	return challenge
}

// auditCountBy counts what somebody did: the installation's log holds every
// test's acts, and every earlier run's.
func (h *harness) auditCountBy(t *testing.T, actorID any, action string) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE actor_user_id = $1 AND action = $2`, actorID, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (h *harness) auditCount(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = $1`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// refusedByCheck says the database itself refused a write as breaking a rule.
func refusedByCheck(t *testing.T, err error, what string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("%s: the database let it through: %v", what, err)
	}
}

// A person sets up an authenticator app, and from then on a password alone
// opens no session: the code it shows does.
func TestASecondFactorIsSetUpAndAskedForAtSignIn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	me := api.client(t)
	signedUp := me.signIn(t, h.person(t, "twostep", auth.RoleOwner))
	email := principalField(t, signedUp, "principal", "user", "email").(string)
	userID := principalField(t, signedUp, "principal", "user", "id").(string)

	status := obj(t, want(t, me.get("/api/v1/auth/me/second-factor"), http.StatusOK, "status before"), "secondFactor")
	if status["enrolled"] != false || status["available"] != true {
		t.Fatalf("status before enrolling = %v", status)
	}

	started := obj(t, want(t, me.post("/api/v1/auth/me/second-factor", nil), http.StatusCreated, "start"), "enrolment")
	secret := started["secret"].(string)
	uri := started["uri"].(string)
	if !strings.HasPrefix(uri, "otpauth://totp/") || !strings.Contains(uri, "secret="+secret) || !strings.Contains(uri, "issuer=Armature") {
		t.Fatalf("the QR code's address is %q", uri)
	}

	// What the database keeps is sealed: neither the text nor the key's bytes.
	var sealed []byte
	if err := h.super.QueryRow(ctx, `SELECT secret_sealed FROM user_second_factor WHERE user_id = $1`, userID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if len(raw) == 0 || bytes.Contains(sealed, raw) || bytes.Contains(bytes.ToUpper(sealed), []byte(secret)) {
		t.Fatal("the authenticator secret is stored in clear")
	}

	// Not in use until a code confirms it.
	want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "an unconfirmed secret asks for nothing yet")

	at := time.Now()
	wrong := me.post("/api/v1/auth/me/second-factor/confirm", map[string]string{"code": wrongCode(t, secret, at)})
	if wrong.Status != http.StatusUnprocessableEntity || wrong.ErrorCode() != "wrong_code" {
		t.Fatalf("a wrong confirming code: %d %s", wrong.Status, wrong.Raw)
	}
	confirmed := want(t, me.post("/api/v1/auth/me/second-factor/confirm", map[string]string{"code": totpCode(t, secret, at)}), http.StatusOK, "confirm")
	codes := list(t, confirmed, "recoveryCodes")
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("recovery codes = %v", codes)
	}
	var keptCodes int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM recovery_code WHERE user_id = $1 AND position($2::bytea in code_hash) = 0`, userID, []byte(codes[0].(string))).Scan(&keptCodes); err != nil {
		t.Fatal(err)
	}
	if keptCodes != auth.RecoveryCodeCount {
		t.Fatalf("%d recovery codes are kept as digests, want %d", keptCodes, auth.RecoveryCodeCount)
	}
	if again := me.post("/api/v1/auth/me/second-factor", nil); again.Status != http.StatusConflict || again.ErrorCode() != "second_factor_on" {
		t.Fatalf("starting over while it is on: %d %s", again.Status, again.Raw)
	}
	status = obj(t, want(t, me.get("/api/v1/auth/me/second-factor"), http.StatusOK, "status after"), "secondFactor")
	if status["enrolled"] != true || status["recoveryCodesLeft"] != float64(auth.RecoveryCodeCount) {
		t.Fatalf("status after enrolling = %v", status)
	}
	if strings.Contains(me.get("/api/v1/auth/me/second-factor").Raw, secret) {
		t.Fatal("the status hands the secret back")
	}
	if h.auditCountBy(t, userID, "auth.second_factor_enrolled") != 1 {
		t.Fatal("enrolling was not recorded")
	}

	t.Run("without the password the account answers like any other", func(t *testing.T) {
		ours := api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": "not the password at all"})
		nobody := api.client(t).post("/api/v1/auth/login", map[string]string{"email": h.email(t, "nobody"), "password": "not the password at all"})
		if ours.Status != http.StatusUnauthorized || ours.Status != nobody.Status || ours.ErrorCode() != nobody.ErrorCode() || ours.Error()["message"] != nobody.Error()["message"] {
			t.Fatalf("a wrong password for an account with a second factor answered %d %s, an unknown address %d %s", ours.Status, ours.Raw, nobody.Status, nobody.Raw)
		}
	})

	t.Run("the password opens nothing until the code", func(t *testing.T) {
		visitor := api.client(t)
		challenge := challengeFor(t, visitor, email)
		want(t, visitor.get("/api/v1/auth/me"), http.StatusUnauthorized, "nobody is signed in on the password alone")

		replayed := visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": totpCode(t, secret, at)})
		if replayed.Status != http.StatusUnprocessableEntity || replayed.ErrorCode() != "wrong_code" {
			t.Fatalf("the code that confirmed was taken again: %d %s", replayed.Status, replayed.Raw)
		}
		signedIn := want(t, visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": totpCode(t, secret, at.Add(auth.TOTPPeriod))}), http.StatusOK, "the next code finishes the sign-in")
		if principalField(t, signedIn, "principal", "user", "email") != email {
			t.Fatalf("signed in as %s", signedIn.Raw)
		}
		want(t, visitor.get("/api/v1/auth/me"), http.StatusOK, "the session works")
		spent := visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": codes[0].(string)})
		if spent.Status != http.StatusGone || spent.ErrorCode() != "challenge_invalid" {
			t.Fatalf("a finished challenge was taken again: %d %s", spent.Status, spent.Raw)
		}
	})

	t.Run("the export says it is on and never holds the secret", func(t *testing.T) {
		export := want(t, me.get("/api/v1/auth/me/export"), http.StatusOK, "export")
		held, _ := export.Body["secondFactor"].(map[string]any)
		if held["enrolled"] != true || strings.Contains(export.Raw, secret) {
			t.Fatalf("the export's second factor = %v", held)
		}
	})

	t.Run("erasing the account takes the factor with it", func(t *testing.T) {
		// The owner cannot be erased while the organization needs them, so a
		// member who joined does it.
		leaving := h.email(t, "leaving")
		invited := want(t, me.post("/api/v1/invites", map[string]any{"email": leaving, "role": "member"}), http.StatusCreated, "invite")
		them := api.client(t)
		joined := want(t, them.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Lee Ving", "password": testPassword}), http.StatusOK, "accept")
		theirID := principalField(t, joined, "principal", "user", "id").(string)
		h.erasable(theirID)
		enrol(t, them)
		challengeFor(t, api.client(t), leaving)
		want(t, them.delete("/api/v1/auth/me"), http.StatusNoContent, "erase")
		var left int
		if err := h.super.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM user_second_factor WHERE user_id = $1)
			     + (SELECT count(*) FROM recovery_code WHERE user_id = $1)
			     + (SELECT count(*) FROM login_challenge WHERE user_id = $1)`, theirID).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Fatalf("%d rows of the second factor outlived the account", left)
		}
	})
}

// A recovery code opens the account once, and the database keeps a used code
// used and a spent step spent, whatever the service forgets.
func TestARecoveryCodeWorksOnceAndTheDatabaseKeepsItSo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	me := api.client(t)
	signedUp := me.signIn(t, h.person(t, "recovery", auth.RoleOwner))
	email := principalField(t, signedUp, "principal", "user", "email").(string)
	userID := principalField(t, signedUp, "principal", "user", "id").(string)
	factor := enrol(t, me)

	first := api.client(t)
	typed := strings.ToUpper(strings.ReplaceAll(factor.codes[0], "-", " "))
	want(t, first.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challengeFor(t, first, email), "code": typed}), http.StatusOK, "a recovery code, however typed, signs in")
	if h.auditCountBy(t, userID, "auth.recovery_code_used") != 1 {
		t.Fatal("using a recovery code was not recorded")
	}

	second := api.client(t)
	challenge := challengeFor(t, second, email)
	if again := second.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": factor.codes[0]}); again.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a used recovery code signed in again: %d %s", again.Status, again.Raw)
	}
	want(t, second.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": factor.codes[1]}), http.StatusOK, "another one does")
	status := obj(t, want(t, me.get("/api/v1/auth/me/second-factor"), http.StatusOK, "status"), "secondFactor")
	if status["recoveryCodesLeft"] != float64(auth.RecoveryCodeCount-2) {
		t.Fatalf("codes left = %v", status["recoveryCodesLeft"])
	}

	_, err := h.super.Exec(ctx, `UPDATE recovery_code SET used_at = NULL WHERE user_id = $1 AND used_at IS NOT NULL`, userID)
	refusedByCheck(t, err, "making a used recovery code unused")
	_, err = h.super.Exec(ctx, `UPDATE user_second_factor SET last_step = last_step - 1 WHERE user_id = $1`, userID)
	refusedByCheck(t, err, "moving the last step back")
	_, err = h.super.Exec(ctx, `UPDATE user_second_factor SET last_step = last_step WHERE user_id = $1`, userID)
	refusedByCheck(t, err, "using the same step again")

	t.Run("a new set replaces the old one", func(t *testing.T) {
		if wrong := me.post("/api/v1/auth/me/second-factor/recovery-codes", map[string]string{"code": "nope-nope-nope-nope"}); wrong.Status != http.StatusUnprocessableEntity || wrong.ErrorCode() != "wrong_code" {
			t.Fatalf("a wrong code renewed the codes: %d %s", wrong.Status, wrong.Raw)
		}
		renewed := want(t, me.post("/api/v1/auth/me/second-factor/recovery-codes", map[string]string{"code": factor.codes[2]}), http.StatusOK, "renew")
		fresh := list(t, renewed, "recoveryCodes")
		if len(fresh) != auth.RecoveryCodeCount {
			t.Fatalf("renewed codes = %v", fresh)
		}
		visitor := api.client(t)
		challenge := challengeFor(t, visitor, email)
		if old := visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": factor.codes[3]}); old.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a code from the old set still works: %d", old.Status)
		}
		want(t, visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": fresh[0].(string)}), http.StatusOK, "a new one does")
		if h.auditCountBy(t, userID, "auth.recovery_codes_renewed") != 1 {
			t.Fatal("renewing was not recorded")
		}
	})
}

// One sign-in takes a few wrong codes and then has to start again with the
// password; the database holds the count as well.
func TestASignInRunsOutOfCodeAttempts(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	me := api.client(t)
	signedUp := me.signIn(t, h.person(t, "attempts", auth.RoleOwner))
	email := principalField(t, signedUp, "principal", "user", "email").(string)
	userID := principalField(t, signedUp, "principal", "user", "id").(string)
	factor := enrol(t, me)

	visitor := api.client(t)
	challenge := challengeFor(t, visitor, email)
	for i := 0; i < auth.ChallengeAttempts; i++ {
		got := visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": wrongCode(t, factor.secret, time.Now())})
		if got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("wrong code %d: %d %s", i+1, got.Status, got.Raw)
		}
	}
	used := visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": factor.next(t)})
	if used.Status != http.StatusGone || used.ErrorCode() != "challenge_invalid" {
		t.Fatalf("the right code after %d wrong ones: %d %s", auth.ChallengeAttempts, used.Status, used.Raw)
	}
	want(t, visitor.get("/api/v1/auth/me"), http.StatusUnauthorized, "still nobody")

	made := api.client(t).post("/api/v1/auth/login/second-factor", map[string]string{"challenge": "made-up", "code": "123456"})
	if made.Status != http.StatusGone {
		t.Fatalf("an invented challenge: %d %s", made.Status, made.Raw)
	}

	again := challengeFor(t, visitor, email)
	_, err := h.super.Exec(context.Background(), `UPDATE login_challenge SET attempts = $2 WHERE user_id = $1`, userID, auth.ChallengeAttempts+1)
	refusedByCheck(t, err, "more wrong codes than a sign-in takes")
	want(t, visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": again, "code": factor.next(t)}), http.StatusOK, "a new sign-in starts the count again")
}

// Taking the factor away needs a code, and ends every other session.
func TestRemovingTheSecondFactorEndsOtherSessions(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	me := api.client(t)
	signedUp := me.signIn(t, h.person(t, "remove", auth.RoleOwner))
	email := principalField(t, signedUp, "principal", "user", "email").(string)
	userID := principalField(t, signedUp, "principal", "user", "id").(string)
	token := want(t, me.post("/api/v1/tokens", map[string]any{"name": "ci", "scopes": []string{}, "projects": []string{}}), http.StatusCreated, "a token")
	viaToken := api.client(t)
	viaToken.bearer = obj(t, token, "token")["secret"].(string)
	factor := enrol(t, me)

	other := api.client(t)
	want(t, other.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challengeFor(t, other, email), "code": factor.next(t)}), http.StatusOK, "a second session")

	if got := viaToken.get("/api/v1/auth/me/second-factor"); got.Status != http.StatusForbidden {
		t.Fatalf("a token read the second factor: %d", got.Status)
	}
	if got := viaToken.post("/api/v1/auth/me/second-factor/remove", map[string]string{"code": factor.codes[0]}); got.Status != http.StatusForbidden {
		t.Fatalf("a token took the second factor away: %d", got.Status)
	}
	if wrong := me.post("/api/v1/auth/me/second-factor/remove", map[string]string{"code": "nope-nope-nope-nope"}); wrong.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a wrong code took it away: %d %s", wrong.Status, wrong.Raw)
	}
	want(t, me.post("/api/v1/auth/me/second-factor/remove", map[string]string{"code": factor.codes[0]}), http.StatusNoContent, "remove")

	want(t, me.get("/api/v1/auth/me"), http.StatusOK, "the session that asked stays")
	if got := other.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
		t.Fatalf("the other session survived: %d", got.Status)
	}
	var left int
	if err := h.super.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_second_factor WHERE user_id = $1) + (SELECT count(*) FROM recovery_code WHERE user_id = $1)`, userID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d rows of the removed factor are left", left)
	}
	want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "the password alone signs in again")
	if gone := me.post("/api/v1/auth/me/second-factor/remove", map[string]string{"code": factor.codes[1]}); gone.Status != http.StatusConflict || gone.ErrorCode() != "no_second_factor" {
		t.Fatalf("removing what is not there: %d %s", gone.Status, gone.Raw)
	}
	if h.auditCountBy(t, userID, "auth.second_factor_removed") != 1 {
		t.Fatal("removing was not recorded")
	}
}

// The installation can require a second factor, at its settings' address; a
// session without one is held to setting it up or signing out, and let go once it has.
func TestTheInstallationRequiresASecondFactor(t *testing.T) {
	h := newHarness(t)
	h.keepSecondFactorRule(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	signedUp := owner.signIn(t, h.person(t, "requires", auth.RoleOwner))
	orgID := principalField(t, signedUp, "principal", "org", "id").(string)
	join := func(prefix, role string) *client {
		t.Helper()
		invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": h.email(t, prefix), "role": role}), http.StatusCreated, "invite")
		them := api.client(t)
		want(t, them.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Joining Person", "password": testPassword}), http.StatusOK, "accept")
		return them
	}
	member := join("held", "member")

	rule := want(t, owner.get("/api/v1/settings/second-factor"), http.StatusOK, "the rule")
	if rule.Body["require"] != "off" {
		t.Fatalf("the installation requires %v before the test, want off", rule.Body["require"])
	}
	if old := owner.get("/api/v1/organization/second-factor"); old.Status != http.StatusNotFound {
		t.Fatalf("the rule still answers at the organization's address: %d %s", old.Status, old.Raw)
	}
	want(t, member.get("/api/v1/settings/second-factor"), http.StatusForbidden, "a member does not set the rule")
	if bad := owner.put("/api/v1/settings/second-factor", map[string]string{"require": "sometimes"}); bad.Status != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown rule: %d %s", bad.Status, bad.Raw)
	}
	if early := owner.put("/api/v1/settings/second-factor", map[string]string{"require": "everyone"}); early.Status != http.StatusConflict || early.ErrorCode() != "enrol_first" {
		t.Fatalf("an owner without a factor required one of everyone: %d %s", early.Status, early.Raw)
	}
	enrol(t, owner)
	want(t, owner.put("/api/v1/settings/second-factor", map[string]string{"require": "everyone"}), http.StatusOK, "require it of everyone")
	if h.auditCountBy(t, principalField(t, signedUp, "principal", "user", "id"), "org.second_factor_required") != 1 {
		t.Fatal("the rule was not recorded")
	}
	_, err := h.super.Exec(context.Background(), `UPDATE installation SET second_factor = 'sometimes' WHERE id = $1`, orgID)
	refusedByCheck(t, err, "a rule the organization cannot have")

	me := want(t, member.get("/api/v1/auth/me"), http.StatusOK, "a held session still says who it is")
	if principalField(t, me, "principal", "mustEnrol") != true {
		t.Fatalf("the member is not told to set it up: %s", me.Raw)
	}
	held := member.get("/api/v1/projects")
	if held.Status != http.StatusForbidden || held.ErrorCode() != "second_factor_required" {
		t.Fatalf("a held session read projects: %d %s", held.Status, held.Raw)
	}
	if words := held.Error()["message"].(string); strings.Contains(words, "organization") {
		t.Errorf("a held session is told about organizations: %q", words)
	}
	if renamed := member.patch("/api/v1/auth/me", map[string]string{"name": "Not Yet"}); renamed.Status != http.StatusForbidden {
		t.Fatalf("a held session changed the profile: %d", renamed.Status)
	}
	want(t, member.get("/api/v1/build"), http.StatusOK, "the foot of the page still loads")
	if build := obj(t, want(t, member.get("/api/v1/meta"), http.StatusOK, "a held session asks which build answers"), "build"); build["commit"] != nil {
		t.Errorf("a held session was told the commit: %v", build)
	}
	want(t, owner.get("/api/v1/projects"), http.StatusOK, "an owner with a factor is not held")

	enrol(t, member)
	want(t, member.get("/api/v1/projects"), http.StatusOK, "enrolling lets the session go")
	if principalField(t, member.get("/api/v1/auth/me"), "principal", "mustEnrol") != nil {
		t.Fatal("the member is still told to set it up")
	}

	t.Run("requiring it of administrators leaves members be", func(t *testing.T) {
		want(t, owner.put("/api/v1/settings/second-factor", map[string]string{"require": "admins"}), http.StatusOK, "administrators only")
		newcomer := join("free", "member")
		want(t, newcomer.get("/api/v1/projects"), http.StatusOK, "a member without a factor works on")
		admin := join("admin", "admin")
		if got := admin.get("/api/v1/projects"); got.Status != http.StatusForbidden {
			t.Fatalf("an administrator without a factor was not held: %d", got.Status)
		}
		want(t, owner.put("/api/v1/settings/second-factor", map[string]string{"require": "off"}), http.StatusOK, "off again")
		want(t, admin.get("/api/v1/projects"), http.StatusOK, "and nobody is held")
	})

	t.Run("a held session can still sign out", func(t *testing.T) {
		want(t, owner.put("/api/v1/settings/second-factor", map[string]string{"require": "admins"}), http.StatusOK, "administrators only")
		admin := join("leaving", "admin")
		want(t, admin.get("/api/v1/auth/me/second-factor"), http.StatusOK, "a held session looks at its factor")
		want(t, admin.post("/api/v1/auth/logout", nil), http.StatusNoContent, "a held session signs out")
		want(t, admin.get("/api/v1/auth/me"), http.StatusUnauthorized, "and is gone")
		want(t, owner.put("/api/v1/settings/second-factor", map[string]string{"require": "off"}), http.StatusOK, "off again")
	})
}

// A provider's sign-in brings the provider's own factor: the installation has
// one provider, so its sessions are not held, while a password session is.
func TestASingleSignOnSessionReliesOnItsProvider(t *testing.T) {
	h := newHarness(t)
	h.keepSecondFactorRule(t)
	h.keepProvider(t)
	ctx := context.Background()
	home := h.newProject(t, "sfhome")
	provider := newIDP(t)
	svc, clientID := configured(t, h, home, provider, false)

	email := h.email(t, "ssofactor")
	h.joinExisting(t, email, "admin")
	if _, err := h.super.Exec(ctx, `UPDATE installation SET second_factor = 'everyone' WHERE id = $1`, home.orgID); err != nil {
		t.Fatal(err)
	}

	session, err := signInWith(t, h, svc, home, provider, clientID, map[string]any{"email": email, "email_verified": true})
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	accounts := h.authService()
	principal, err := accounts.Authenticate(ctx, session.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Org == nil || principal.Org.ID != home.orgID || principal.MustEnrol {
		t.Fatalf("the provider's sign-in was held: %+v", principal)
	}

	// The owner signed up with a password and has no factor: held.
	byPassword, err := accounts.Authenticate(ctx, home.owner.SessionSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !byPassword.MustEnrol {
		t.Fatal("a password session without a factor went unheld")
	}
}

// An administrator takes away the factor of somebody who lost the phone and
// the codes, in the reach they have over that person's password.
func TestAnAdministratorTakesAwayALostSecondFactor(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	signedUp := owner.signIn(t, h.person(t, "lostphone", auth.RoleOwner))
	ownerID := principalField(t, signedUp, "principal", "user", "id").(string)
	email := h.email(t, "lost")
	made := want(t, owner.post("/api/v1/users", map[string]string{"email": email, "name": "Lost Phone", "role": "member", "password": testPassword}), http.StatusCreated, "a local user")
	userID := obj(t, made, "user")["id"].(string)

	them := api.client(t)
	want(t, them.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "they sign in")
	enrol(t, them)

	if got := them.delete("/api/v1/users/" + ownerID + "/second-factor"); got.Status != http.StatusForbidden {
		t.Fatalf("a member took somebody's factor away: %d", got.Status)
	}
	if got := owner.delete("/api/v1/users/" + ownerID + "/second-factor"); got.Status != http.StatusConflict {
		t.Fatalf("an administrator's own factor went this way: %d %s", got.Status, got.Raw)
	}
	h.waitForPrimary(t)
	if !listedWithFactor(t, owner, email, userID) {
		t.Fatal("the users list does not say the factor is on")
	}
	want(t, owner.delete("/api/v1/users/"+userID+"/second-factor"), http.StatusNoContent, "take it away")
	h.waitForPrimary(t)
	if listedWithFactor(t, owner, email, userID) {
		t.Fatal("the users list still says the factor is on")
	}
	if again := owner.delete("/api/v1/users/" + userID + "/second-factor"); again.Status != http.StatusConflict || again.ErrorCode() != "no_second_factor" {
		t.Fatalf("taking away what is not there: %d %s", again.Status, again.Raw)
	}
	want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "the password alone signs them in")
	if h.auditCountBy(t, ownerID, "member.second_factor_removed") != 1 {
		t.Fatal("the removal was not recorded")
	}
}

// A server with no ARMATURE_SECRET_KEY has nowhere safe to keep a secret, and
// says so rather than keeping it in clear.
func TestWithoutAKeyNoSecondFactorIsSetUp(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	api.api.Auth = h.authServiceWithoutKey()

	me := api.client(t)
	me.signIn(t, h.person(t, "nokey", auth.RoleOwner))
	refused := me.post("/api/v1/auth/me/second-factor", nil)
	if refused.Status != http.StatusServiceUnavailable || refused.ErrorCode() != "second_factor_unavailable" || !strings.Contains(refused.Raw, "ARMATURE_SECRET_KEY") {
		t.Fatalf("enrolling without a key: %d %s", refused.Status, refused.Raw)
	}
	status := obj(t, want(t, me.get("/api/v1/auth/me/second-factor"), http.StatusOK, "status"), "secondFactor")
	if status["available"] != false {
		t.Fatalf("the status says it is available: %v", status)
	}
}

// listedWithFactor reads the users list for whether somebody has a factor on.
func listedWithFactor(t *testing.T, owner *client, email, userID string) bool {
	t.Helper()
	for _, row := range list(t, want(t, owner.get("/api/v1/users?q="+url.QueryEscape(email)), http.StatusOK, "list users"), "users") {
		if u := row.(map[string]any); u["id"] == userID {
			return u["secondFactor"] == true
		}
	}
	t.Fatalf("%s is not listed", userID)
	return false
}
