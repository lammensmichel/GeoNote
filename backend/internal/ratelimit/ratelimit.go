// Package ratelimit fournit les limiteurs utilises par les middlewares
// RateLimit (seau a jetons par IP) et AntiSpam (delai entre deux actions).
//
// Deux implementations existent :
//   - memoire : un seul processus, utile en dev ou si Redis est absent ;
//   - Redis   : partagee entre toutes les instances (necessaire en serverless,
//     ou chaque instance a sa propre memoire).
//
// La version Redis retombe sur la version memoire si Redis ne repond pas :
// l'API reste disponible, avec une limite par instance seulement.
package ratelimit

import (
	"context"
	"log"
	"sync"
	"time"
)

// Limiter decide si une action identifiee par key est autorisee maintenant.
type Limiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

// breakerPause : apres une erreur Redis, on reste en memoire pendant cette
// duree sans reessayer Redis, pour ne pas payer un delai a chaque requete.
const breakerPause = 10 * time.Second

// fallback utilise primary et passe a secondary quand primary renvoie une erreur.
type fallback struct {
	name      string
	primary   Limiter
	secondary Limiter
	now       func() time.Time

	mu        sync.Mutex
	openUntil time.Time
}

// WithFallback renvoie un limiteur qui utilise secondary si primary echoue.
// Apres une erreur, primary n'est plus appele pendant breakerPause.
// Chaque bascule est journalisee (donc au plus une fois par breakerPause).
func WithFallback(name string, primary, secondary Limiter) Limiter {
	return &fallback{name: name, primary: primary, secondary: secondary, now: time.Now}
}

func (f *fallback) Allow(ctx context.Context, key string) (bool, error) {
	f.mu.Lock()
	open := f.now().Before(f.openUntil)
	f.mu.Unlock()
	if !open {
		ok, err := f.primary.Allow(ctx, key)
		if err == nil {
			return ok, nil
		}
		f.mu.Lock()
		if !f.now().Before(f.openUntil) {
			f.openUntil = f.now().Add(breakerPause)
			log.Printf("[ratelimit] %s: Redis indisponible, limite en memoire pendant %s: %v", f.name, breakerPause, err)
		}
		f.mu.Unlock()
	}
	return f.secondary.Allow(ctx, key)
}
