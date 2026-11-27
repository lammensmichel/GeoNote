package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func corsOrigin(t *testing.T, allowed []string, origin string) string {
	t.Helper()
	h := CORS(allowed)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("Origin", origin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Header().Get("Access-Control-Allow-Origin")
}

func TestCORSEmptyListAllowsNoOrigin(t *testing.T) {
	if got := corsOrigin(t, nil, "https://evil.example"); got != "" {
		t.Fatalf("liste vide : origine acceptee (%q), doit etre refusee", got)
	}
}

func TestCORSAllowsOnlyListedOrigins(t *testing.T) {
	allowed := []string{"https://app.example.be"}
	if got := corsOrigin(t, allowed, "https://app.example.be"); got != "https://app.example.be" {
		t.Fatalf("origine listee refusee: %q", got)
	}
	if got := corsOrigin(t, allowed, "https://evil.example"); got != "" {
		t.Fatalf("origine non listee acceptee: %q", got)
	}
}

func TestCORSDevLocalhost(t *testing.T) {
	allowed := []string{"http://localhost:*", "http://127.0.0.1:*"}
	if got := corsOrigin(t, allowed, "http://localhost:8080"); got != "http://localhost:8080" {
		t.Fatalf("localhost refuse en dev: %q", got)
	}
}
