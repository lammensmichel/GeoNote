package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

// CORS autorise uniquement les origines listees (appli web, page d'accueil).
// Attention : go-chi/cors traite une liste vide comme « toutes les origines ».
// Ici, une liste vide refuse au contraire tout appel depuis un navigateur ;
// l'appli mobile n'envoie pas d'en-tete Origin et n'est pas concernee.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	opts := cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", RequestIDHeader},
		ExposedHeaders:   []string{"Link", RequestIDHeader},
		AllowCredentials: true,
		MaxAge:           300,
	}
	if len(allowedOrigins) == 0 {
		opts.AllowOriginFunc = func(*http.Request, string) bool { return false }
	}
	return cors.Handler(opts)
}
