package middleware

import (
	"net"
	"net/http"
	"strings"
)

// TrustedRealIP remplace RemoteAddr par l'IP lue dans header, un en-tete que
// le proxy devant l'API ecrit lui-meme (ex. "X-Real-IP" sur Vercel).
//
// Contrairement a chimw.RealIP, aucun autre en-tete n'est lu : True-Client-IP,
// X-Forwarded-For, etc. viennent du client et permettraient de changer d'IP a
// chaque requete pour contourner la limite. Si header est vide (dev, Docker
// sans proxy), RemoteAddr est garde tel quel.
//
// Si l'en-tete contient une liste, on prend la derniere entree : c'est celle
// ajoutee par le proxy, les precedentes viennent du client.
func TrustedRealIP(header string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if header == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if v := r.Header.Get(header); v != "" {
				parts := strings.Split(v, ",")
				ip := strings.TrimSpace(parts[len(parts)-1])
				if net.ParseIP(ip) != nil {
					r.RemoteAddr = ip
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
