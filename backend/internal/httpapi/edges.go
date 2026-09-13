package httpapi

import (
	"net/http"
	"strings"
)

// securityHeaders says what a browser should refuse to do with an answer of
// ours. The API answers JSON, so it needs nothing of its own to run.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

// sameSite refuses a cookie-carried write that a browser was talked into from
// elsewhere: it asks for JSON, which a cross-site form cannot send.
func (s *Server) sameSite(allowed []string) func(http.Handler) http.Handler {
	origins := map[string]bool{}
	for _, origin := range append(allowed, s.AppBaseURL) {
		if origin = strings.TrimRight(strings.TrimSpace(origin), "/"); origin != "" {
			origins[origin] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if safeMethod(r.Method) || !carriesCookie(r, s.CookieName) {
				next.ServeHTTP(w, r)
				return
			}
			if origin := strings.TrimRight(r.Header.Get("Origin"), "/"); origin != "" && s.CheckOrigin && !origins[origin] {
				respondError(w, r, &APIError{Status: http.StatusForbidden, Code: "cross_site",
					Message: "That request came from another site."})
				return
			}
			if kind := r.Header.Get("Content-Type"); kind != "" && !bodyKindAllowed(kind) {
				respondError(w, r, &APIError{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type",
					Message: "Send JSON, or a file as multipart form data."})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// carriesCookie says whether this request rides on the browser's session
// cookie; a request that carries its own token was never talked into anything.
func carriesCookie(r *http.Request, name string) bool {
	if r.Header.Get("Authorization") != "" {
		return false
	}
	_, err := r.Cookie(name)
	return err == nil
}

func bodyKindAllowed(contentType string) bool {
	kind := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return kind == "application/json" || kind == "multipart/form-data"
}
