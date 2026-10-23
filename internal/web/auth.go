package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
)

const cookieName = "modelpick_ui"

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

// guard enforces Host, Origin and the token cookie before next runs.
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
		if c, err := r.Cookie(cookieName); err == nil && tokenEqual(c.Value, token) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/" && tokenEqual(r.URL.Query().Get("t"), token) {
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/",
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
