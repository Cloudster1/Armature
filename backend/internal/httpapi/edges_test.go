package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A cross-site form can post to this API with the browser's cookie attached.
// It cannot send JSON, and in production it cannot claim one of our origins.
func TestACookieCarriedWriteComesFromHere(t *testing.T) {
	cases := []struct {
		name, method, contentType, origin string
		cookie, checkOrigin               bool
		want                              int
	}{
		{name: "a form post with a cookie is refused", method: http.MethodPost, contentType: "text/plain", cookie: true, want: http.StatusUnsupportedMediaType},
		{name: "JSON with a cookie is served", method: http.MethodPost, contentType: "application/json", cookie: true, want: http.StatusOK},
		{name: "a file upload with a cookie is served", method: http.MethodPost, contentType: "multipart/form-data; boundary=x", cookie: true, want: http.StatusOK},
		{name: "a form post with a token is the client's own doing", method: http.MethodPost, contentType: "text/plain", want: http.StatusOK},
		{name: "reading is never refused", method: http.MethodGet, contentType: "text/plain", cookie: true, want: http.StatusOK},
		{name: "another site's origin is refused in production", method: http.MethodPost, contentType: "application/json", origin: "https://evil.test", cookie: true, checkOrigin: true, want: http.StatusForbidden},
		{name: "our own origin is served", method: http.MethodPost, contentType: "application/json", origin: "https://armature.test", cookie: true, checkOrigin: true, want: http.StatusOK},
		{name: "in development the origin is not asked about", method: http.MethodPost, contentType: "application/json", origin: "http://localhost:5173", cookie: true, want: http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Server{CookieName: "armature_session", AppBaseURL: "https://armature.test", CheckOrigin: c.checkOrigin}
			handler := s.sameSite(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
			r := httptest.NewRequest(c.method, "/api/v1/issues", strings.NewReader("{}"))
			r.Header.Set("Content-Type", c.contentType)
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if c.cookie {
				r.AddCookie(&http.Cookie{Name: "armature_session", Value: "s"})
			} else {
				r.Header.Set("Authorization", "Bearer armature_pat_x")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}

// Every answer says what a browser should refuse to do with it.
func TestEveryAnswerCarriesItsRefusals(t *testing.T) {
	rec := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("policy = %q", rec.Header().Get("Content-Security-Policy"))
	}
}
