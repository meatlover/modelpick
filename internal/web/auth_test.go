package web

import "testing"

func TestTokenEqual(t *testing.T) {
	if !tokenEqual("a", "a") || tokenEqual("a", "b") || tokenEqual("", "a") {
		t.Error("tokenEqual wrong")
	}
}

func TestAllowedHostAndOrigin(t *testing.T) {
	if !allowedHost("localhost:9", 9) || allowedHost("localhost:8", 9) || allowedHost("evil:9", 9) {
		t.Error("host")
	}
	if !allowedOrigin("http://127.0.0.1:9", 9) || allowedOrigin("https://127.0.0.1:9", 9) || allowedOrigin("http://evil", 9) {
		t.Error("origin")
	}
}
