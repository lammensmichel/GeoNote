package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequestIDGenerated(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetRequestID(r.Context())
	}))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rec.Header().Get(RequestIDHeader)
	if len(got) != 32 {
		t.Fatalf("id genere attendu de 32 caracteres, recu %q", got)
	}
	if seen != got {
		t.Fatalf("id du contexte %q different de l'en-tete %q", seen, got)
	}
}

func TestRequestIDReusesValidClientID(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "abc-123_XYZ")
	if got := serve(h, req).Header().Get(RequestIDHeader); got != "abc-123_XYZ" {
		t.Fatalf("id client valide non repris: %q", got)
	}
}

func TestRequestIDRejectsInvalidClientID(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, bad := range []string{"has space", "x\ny", strings.Repeat("a", 65), "<script>"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(RequestIDHeader, bad)
		if got := serve(h, req).Header().Get(RequestIDHeader); got == bad {
			t.Errorf("id invalide %q repris tel quel", bad)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/", nil))
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, attendu %q", k, got, want)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy manquant")
	}
}
