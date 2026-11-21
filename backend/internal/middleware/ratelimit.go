package middleware

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/mimi6060/GeoNote/backend/internal/ratelimit"
)

func writeTooMany(w http.ResponseWriter, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

// clientIP renvoie l'IP du client sans le port. chimw.RealIP remplace deja
// RemoteAddr par l'IP du proxy en amont ; sinon RemoteAddr vaut "ip:port" et
// chaque connexion aurait sa propre limite.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// RateLimit limite le nombre de requetes par IP (seau a jetons).
// Si le limiteur renvoie une erreur, la requete passe : mieux vaut servir
// que bloquer tout le monde (le limiteur Redis a deja son repli en memoire).
func RateLimit(limiter ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, err := limiter.Allow(r.Context(), clientIP(r))
			if err == nil && !ok {
				writeTooMany(w, "RATE_LIMIT", "Trop de requetes")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AntiSpam limite un utilisateur connecte a une action par delai
// (ex. creation de message). Les requetes anonymes ne sont pas concernees.
func AntiSpam(limiter ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r.Context())
			if userID == "" {
				next.ServeHTTP(w, r)
				return
			}
			ok, err := limiter.Allow(r.Context(), userID)
			if err == nil && !ok {
				writeTooMany(w, "SPAM_LIMIT", "Attendez avant de poster a nouveau")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
