package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
)

// cookieName is per port: cookies on 127.0.0.1 are shared by every port,
// so two servers (or a stale one) must not overwrite each other's.
func cookieName(port int) string { return "modelpick_ui_" + strconv.Itoa(port) }

// NewToken returns 32 random bytes, hex encoded.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func tokenEqual(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func allowedHost(host string, port int) bool {
	p := ":" + strconv.Itoa(port)
	return host == "127.0.0.1"+p || host == "localhost"+p
}

func allowedOrigin(origin string, port int) bool {
	p := ":" + strconv.Itoa(port)
	return origin == "http://127.0.0.1"+p || origin == "http://localhost"+p
}

// guard enforces Host, Origin and the token cookie before next runs. The
// token in the query is accepted on GET / (which sets the cookie) and on GET
// /api/ping only.
func guard(next http.Handler, port int, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHost(r.Host, port) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if !originOK(r, port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if c, err := r.Cookie(cookieName(port)); err == nil && tokenEqual(c.Value, token) {
			next.ServeHTTP(w, r)
			return
		}
		queryToken := r.Method == http.MethodGet && tokenEqual(r.URL.Query().Get("t"), token)
		// The single-instance check (spec §8.1) holds the token, not the cookie.
		if queryToken && r.URL.Path == "/api/ping" {
			next.ServeHTTP(w, r)
			return
		}
		if queryToken && r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{Name: cookieName(port), Value: token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// originOK: a present Origin must be ours; a non-GET request must carry one.
func originOK(r *http.Request, port int) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	}
	return allowedOrigin(o, port)
}
