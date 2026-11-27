package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func realIPOf(t *testing.T, header string, set map[string]string) string {
	t.Helper()
	var got string
	h := TrustedRealIP(header)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	for k, v := range set {
		req.Header.Set(k, v)
	}
	serve(h, req)
	return got
}

func TestTrustedRealIPDisabledIgnoresHeaders(t *testing.T) {
	got := realIPOf(t, "", map[string]string{
		"True-Client-IP":  "1.2.3.4",
		"X-Real-IP":       "1.2.3.4",
		"X-Forwarded-For": "1.2.3.4",
	})
	if got != "10.0.0.1:1234" {
		t.Fatalf("RemoteAddr = %q, attendu inchange", got)
	}
}

func TestTrustedRealIPOnlyReadsConfiguredHeader(t *testing.T) {
	got := realIPOf(t, "X-Real-IP", map[string]string{
		"True-Client-IP": "6.6.6.6", // fourni par le client : ignore
		"X-Real-IP":      "203.0.113.9",
	})
	if got != "203.0.113.9" {
		t.Fatalf("RemoteAddr = %q, attendu 203.0.113.9", got)
	}
}

func TestTrustedRealIPTakesLastEntry(t *testing.T) {
	got := realIPOf(t, "X-Forwarded-For", map[string]string{
		"X-Forwarded-For": "6.6.6.6, 203.0.113.9",
	})
	if got != "203.0.113.9" {
		t.Fatalf("RemoteAddr = %q, attendu 203.0.113.9", got)
	}
}

func TestTrustedRealIPRejectsGarbage(t *testing.T) {
	got := realIPOf(t, "X-Real-IP", map[string]string{"X-Real-IP": "pas-une-ip"})
	if got != "10.0.0.1:1234" {
		t.Fatalf("RemoteAddr = %q, attendu inchange", got)
	}
}
