package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
)

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
		writeJSON(w, http.StatusOK, b.State())
	})
	mux.HandleFunc("GET /api/usage", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, b.Usage())
	})
	mux.HandleFunc("GET /api/doctor", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, b.Doctor())
	})
	// The catch-all "/" below would otherwise turn a wrong method into 404.
	mux.HandleFunc("/api/usage/{account}/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		refreshHandler(b)(w, r)
	})
	mux.HandleFunc("POST /api/op", opHandler(b))
	mux.Handle("/", http.FileServerFS(sub))
	return withHeaders(guard(mux, port, token))
}

func refreshHandler(b Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		row, err := b.Refresh(r.PathValue("account"))
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"reason": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, row)
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
		var c Conflict
		var ref Refused
		switch {
		case errors.As(err, &c):
			writeJSON(w, http.StatusConflict, map[string]string{"sha256": c.SHA256})
		case errors.As(err, &ref):
			body := map[string]any{"reason": ref.Reason}
			if ref.Uses > 0 {
				body["uses"] = ref.Uses
			}
			writeJSON(w, http.StatusUnprocessableEntity, body)
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"reason": err.Error()})
		default:
			writeJSON(w, http.StatusOK, res)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // a failed write means the client left
}

// withHeaders sets the CSP (and no CORS) before anything else can respond.
func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
