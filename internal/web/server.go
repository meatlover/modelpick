package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
)

// errorLog receives the detail of every error the page is not shown.
var errorLog = log.New(os.Stderr, "modelpick ui: ", log.LstdFlags)

//go:embed static
var staticFS embed.FS

// NewServer returns the page's handler. port is what Host and Origin are
// checked against; token is the only credential.
func NewServer(b Backend, port int, token string) http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // the embedded tree always has static/
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, normalState(b.State()))
	})
	mux.HandleFunc("GET /api/usage", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, normalUsage(b.Usage()))
	})
	mux.HandleFunc("GET /api/doctor", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, normalDoctor(b.Doctor()))
	})
	mux.HandleFunc("/api/usage/{account}/refresh", postOnly(refreshHandler(b)))
	mux.HandleFunc("/api/doctor/all", postOnly(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, normalDoctor(b.DoctorAll()))
	}))
	mux.HandleFunc("POST /api/op", opHandler(b))
	mux.Handle("/", http.FileServerFS(sub))
	return withHeaders(guard(mux, port, token))
}

// postOnly answers any other method 405 with Allow: POST. The routes it
// wraps carry no method, because the catch-all "/" would otherwise turn a
// wrong method into 404.
func postOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}

func refreshHandler(b Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account := r.PathValue("account")
		row, err := b.Refresh(account)
		if err != nil {
			reason := account + ": refresh failed"
			if ref, ok := asRefused(err); ok {
				reason = ref.Reason
			} else {
				errorLog.Printf("refresh %s: %v", account, err)
			}
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"reason": reason})
			return
		}
		writeJSON(w, http.StatusOK, normalRow(row))
	}
}

func opHandler(b Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req OpRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"reason": "bad request body"})
			return
		}
		res, err := b.Apply(req)
		if err == nil {
			writeJSON(w, http.StatusOK, normalOpResult(res))
			return
		}
		if c, ok := asConflict(err); ok {
			writeJSON(w, http.StatusConflict, map[string]string{"sha256": c.SHA256})
			return
		}
		if ref, ok := asRefused(err); ok {
			body := map[string]any{"reason": ref.Reason}
			if ref.Uses > 0 {
				body["uses"] = ref.Uses
			}
			writeJSON(w, http.StatusUnprocessableEntity, body)
			return
		}
		// Final review Important 5: the detail can hold a path or a vendor's
		// words; the page gets a fixed text and stderr gets the error.
		errorLog.Printf("op %s: %v", req.Op, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"reason": "internal error"})
	}
}

// asConflict finds a Conflict in err's chain, as a value or a pointer.
func asConflict(err error) (Conflict, bool) {
	var v Conflict
	if errors.As(err, &v) {
		return v, true
	}
	var p *Conflict
	if errors.As(err, &p) && p != nil {
		return *p, true
	}
	return Conflict{}, false
}

// asRefused finds a Refused in err's chain, as a value or a pointer.
func asRefused(err error) (Refused, bool) {
	var v Refused
	if errors.As(err, &v) {
		return v, true
	}
	var p *Refused
	if errors.As(err, &p) && p != nil {
		return *p, true
	}
	return Refused{}, false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // a failed write means the client left
}

// withHeaders sets the CSP (and no CORS) before anything else can respond;
// frame-ancestors 'none' keeps the page out of another site's frame.
func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
