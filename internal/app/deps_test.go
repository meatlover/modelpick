package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientDoesNotFollowRedirects(t *testing.T) {
	var reached int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++ }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	c := newHTTPClient()
	if c.Timeout != 10*time.Second {
		t.Fatalf("timeout = %v, want 10s", c.Timeout)
	}
	resp, err := c.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || reached != 0 {
		t.Fatalf("status %d, redirect target reached %d times; want 302 and 0", resp.StatusCode, reached)
	}
}
