package httpapi

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/desk"
)

// The portal's door for people without an account: the desk's name, a code by
// mail, and a session for the code. Nobody is signed in on these routes, so the
// organization comes from the address in the URL.

// codeRequests is the brake on mailing codes from one address: enough for a
// person, too little for a flood. It is per process, which is what a brake
// needs to be; a quota would be a table.
var codeRequests = newThrottle(CodeRequestsPerWindow, DoorWindow)

// openEntries is the same brake on walking in through an open door, where
// every entry is an account.
var openEntries = newThrottle(OpenEntriesPerWindow, DoorWindow)

// codeRedemptions is the brake on typing codes from one address, a little
// looser than asking for them, since a person may mistype.
var codeRedemptions = newThrottle(CodeRedemptionsPerWindow, DoorWindow)

// The door's brakes, per address per window.
const (
	DoorWindow               = 10 * time.Minute
	CodeRequestsPerWindow    = 10
	OpenEntriesPerWindow     = 30
	CodeRedemptionsPerWindow = 30
	// CredentialTriesPerWindow brakes guessing at one account's password, by
	// the address guessed at: an office shares one address, a guesser moves.
	CredentialTriesPerWindow = 10
	// throttleMaxKeys bounds what a brake remembers at once.
	throttleMaxKeys = 10000
)

// credentialTries brakes password and invitation guessing, by address.
var credentialTries = newThrottle(CredentialTriesPerWindow, DoorWindow)

type throttle struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	seen   map[string][]time.Time
}

func newThrottle(limit int, window time.Duration) *throttle {
	return &throttle{limit: limit, window: window, seen: map[string][]time.Time{}}
}

// reset forgets everything the brake has seen.
func (t *throttle) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seen = map[string][]time.Time{}
}

// ResetThrottles forgets the door's brakes, for a test suite whose every
// caller is one address in one process.
func ResetThrottles() {
	codeRequests.reset()
	openEntries.reset()
	codeRedemptions.reset()
	credentialTries.reset()
}

// allow records one request from a key and says whether it was inside the limit.
// An address is forgotten as soon as its window has passed, so the brake
// holds nothing about anyone who is not knocking.
func (t *throttle) allow(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for other, times := range t.seen {
		if other != key && len(times) > 0 && now.Sub(times[len(times)-1]) >= t.window {
			delete(t.seen, other)
		}
	}
	// A caller who names a new key every time must not be able to grow this
	// map without bound; the oldest go first, and they were quiet anyway.
	if len(t.seen) > throttleMaxKeys {
		for other, times := range t.seen {
			if other != key && (len(times) == 0 || now.Sub(times[len(times)-1]) > t.window/2) {
				delete(t.seen, other)
			}
		}
	}
	kept := t.seen[key][:0]
	for _, at := range t.seen[key] {
		if now.Sub(at) < t.window {
			kept = append(kept, at)
		}
	}
	if len(kept) >= t.limit {
		t.seen[key] = kept
		return false
	}
	t.seen[key] = append(kept, now)
	return true
}

// over says whether a key has used up its window, without counting this look:
// a brake on guessing counts misses, and knowing your own password is not one.
func (t *throttle) over(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	kept := t.seen[key][:0]
	for _, at := range t.seen[key] {
		if now.Sub(at) < t.window {
			kept = append(kept, at)
		}
	}
	t.seen[key] = kept
	return len(kept) >= t.limit
}

// record counts one miss against a key.
func (t *throttle) record(key string) { t.allow(key, time.Now()) }

// tooMany answers a caller who has missed too often and says whether it did.
func (t *throttle) tooMany(w http.ResponseWriter, r *http.Request, key, message string) bool {
	if !t.over(key, time.Now()) {
		return false
	}
	respondError(w, r, &APIError{Status: http.StatusTooManyRequests, Code: "too_many_requests", Message: message})
	return true
}

// gate answers a request that is over the limit and says whether it may go on.
func (t *throttle) gate(w http.ResponseWriter, r *http.Request, message string) bool {
	return t.gateKey(w, r, clientIP(r), message)
}

// gateKey is gate against something other than the caller's address, such as
// the address somebody is guessing the password of.
func (t *throttle) gateKey(w http.ResponseWriter, r *http.Request, key, message string) bool {
	if t.allow(key, time.Now()) {
		return true
	}
	respondError(w, r, &APIError{Status: http.StatusTooManyRequests, Code: "too_many_requests", Message: message})
	return false
}

// deskBySlug names the organization behind a portal address, or says there is
// no desk there; an unknown slug and an archived one are the same answer.
func (s *Server) deskBySlug(r *http.Request) (*auth.Org, *APIError) {
	org, err := s.Auth.OrgBySlug(r.Context(), r.PathValue("orgSlug"))
	if err != nil {
		return nil, ErrNotFound("There is no service desk at that address.")
	}
	return org, nil
}

func (s *Server) handleDeskEntry(w http.ResponseWriter, r *http.Request) {
	org, apiErr := s.deskBySlug(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	open, err := s.Desk.OpenDoors(r.Context(), org.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"name": org.Name, "slug": org.Slug, "open": open})
}

type portalCodeRequest struct {
	Email string `json:"email"`
}

// handlePortalCode mails a code. The answer is the same for every well-formed
// address, known or not, so nothing can be learned by asking.
func (s *Server) handlePortalCode(w http.ResponseWriter, r *http.Request) {
	org, apiErr := s.deskBySlug(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req portalCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if s.Mailer == nil {
		respondError(w, r, &APIError{Status: http.StatusServiceUnavailable, Code: "mail_unavailable",
			Message: "Mail is not set up on this server, so the portal cannot send codes. Ask whoever runs it."})
		return
	}
	if !codeRequests.gate(w, r, "Too many codes were asked for from here. Wait a few minutes and try again.") {
		return
	}
	code, expiresAt, err := s.Auth.IssuePortalCode(r.Context(), org.ID, req.Email)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if err := s.Mailer.Send(r.Context(), desk.CodeMail(req.Email, org.Name, code)); err != nil {
		s.Log.Warn("portal code could not be mailed", "error", err)
		respondError(w, r, &APIError{Status: http.StatusBadGateway, Code: "mail_failed",
			Message: "The code could not be sent. Try again in a minute."})
		return
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"expiresInSeconds": int(time.Until(expiresAt).Seconds())})
}

type portalSessionRequest struct {
	Email string `json:"email"`
	// Code is the mailed code; with Desk instead, the desk's open door is
	// walked through with Name and no code.
	Code string `json:"code,omitempty"`
	Desk string `json:"desk,omitempty"`
	Name string `json:"name,omitempty"`
}

// handlePortalSession trades a code for a session, the way a login does, or
// lets somebody straight into a desk whose door is open.
func (s *Server) handlePortalSession(w http.ResponseWriter, r *http.Request) {
	org, apiErr := s.deskBySlug(r)
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req portalSessionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	var (
		creds *auth.Credentials
		err   error
	)
	switch {
	case req.Desk != "" && req.Code != "":
		respondError(w, r, ErrBadRequest("Give either a code or a desk, not both."))
		return
	case req.Desk != "":
		if !openEntries.gate(w, r, "Too many people came in from here. Wait a few minutes and try again.") {
			return
		}
		creds, err = s.Auth.OpenPortalSession(r.Context(), auth.OpenSessionInput{
			OrgID: org.ID, ProjectKey: req.Desk, Email: req.Email, Name: req.Name, UserAgent: r.UserAgent(), IP: clientIP(r),
		})
	default:
		if !codeRedemptions.gate(w, r, "Too many codes were tried from here. Wait a few minutes and try again.") {
			return
		}
		creds, err = s.Auth.RedeemPortalCode(r.Context(), auth.PortalSessionInput{
			OrgID: org.ID, Email: req.Email, Code: req.Code, UserAgent: r.UserAgent(), IP: clientIP(r),
		})
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.setSessionCookie(w, creds.SessionSecret, creds.ExpiresAt)
	s.Fresh.Note(r.Context(), "s:"+creds.Principal.SessionID.String(), creds.LSN)
	respondJSON(w, r, http.StatusOK, map[string]any{"principal": creds.Principal})
}

// portalEntryError maps the code path's refusals; it is called from toAPIError.
func portalEntryError(err error) *APIError {
	switch {
	case errors.Is(err, auth.ErrBadAddress):
		return ErrValidation(map[string]string{"email": "That is not a mail address. Write it as name@example.com."})
	case errors.Is(err, auth.ErrCodeTooSoon):
		return &APIError{Status: http.StatusTooManyRequests, Code: "too_many_requests",
			Message: "A code was sent less than a minute ago. Check your mail before asking for another."}
	case errors.Is(err, auth.ErrCodeInvalid):
		return &APIError{Status: http.StatusUnauthorized, Code: "code_invalid",
			Message: "That code is wrong or has expired. Ask for a new one."}
	case errors.Is(err, auth.ErrHasAccount):
		return &APIError{Status: http.StatusForbidden, Code: "has_account",
			Message: "You have an account here. Sign in with it."}
	case errors.Is(err, auth.ErrDoorAsksForCode):
		return &APIError{Status: http.StatusForbidden, Code: "door_asks_for_code",
			Message: "This desk asks for a code by mail first. Ask for one with your address."}
	}
	return nil
}
