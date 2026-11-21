package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recorder garde les cles demandees et renvoie une reponse fixe.
type recorder struct {
	keys []string
	ok   bool
	err  error
}

func (r *recorder) Allow(_ context.Context, key string) (bool, error) {
	r.keys = append(r.keys, key)
	return r.ok, r.err
}

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRateLimitKeyIsIPWithoutPort(t *testing.T) {
	l := &recorder{ok: true}
	h := RateLimit(l)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	serve(h, req)

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "2001:db8::1" // apres chimw.RealIP : pas de port
	serve(h, req)

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8::2]:443"
	serve(h, req)

	want := []string{"203.0.113.7", "2001:db8::1", "2001:db8::2"}
	for i, k := range want {
		if l.keys[i] != k {
			t.Fatalf("cle %d = %q, attendu %q", i, l.keys[i], k)
		}
	}
}

func TestRateLimitBlocks(t *testing.T) {
	rec := serve(RateLimit(&recorder{ok: false})(okHandler), httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("code %d, attendu 429", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "RATE_LIMIT") {
		t.Fatalf("corps inattendu: %s", rec.Body.String())
	}
}

func TestRateLimitLetsThroughOnError(t *testing.T) {
	rec := serve(RateLimit(&recorder{err: errors.New("boom")})(okHandler), httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d, attendu 200", rec.Code)
	}
}

func TestAntiSpamSkipsAnonymous(t *testing.T) {
	l := &recorder{ok: false}
	rec := serve(AntiSpam(l)(okHandler), httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusOK || len(l.keys) != 0 {
		t.Fatalf("anonyme: code %d, appels %d ; attendu 200 et 0 appel", rec.Code, len(l.keys))
	}
}

func TestAntiSpamUsesUserID(t *testing.T) {
	l := &recorder{ok: false}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), UserIDKey, "user-42"))
	rec := serve(AntiSpam(l)(okHandler), req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("code %d, attendu 429", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SPAM_LIMIT") {
		t.Fatalf("corps inattendu: %s", rec.Body.String())
	}
	if len(l.keys) != 1 || l.keys[0] != "user-42" {
		t.Fatalf("cles %v, attendu [user-42]", l.keys)
	}
}
