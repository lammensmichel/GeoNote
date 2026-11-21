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

// fallback utilise primary et passe a secondary quand primary renvoie une erreur.
type fallback struct {
	name      string
	primary   Limiter
	secondary Limiter

	mu      sync.Mutex
	lastLog time.Time
}

// WithFallback renvoie un limiteur qui utilise secondary si primary echoue.
// Les erreurs sont journalisees au plus une fois par minute.
func WithFallback(name string, primary, secondary Limiter) Limiter {
	return &fallback{name: name, primary: primary, secondary: secondary}
}

func (f *fallback) Allow(ctx context.Context, key string) (bool, error) {
	ok, err := f.primary.Allow(ctx, key)
	if err == nil {
		return ok, nil
	}
	f.mu.Lock()
	if time.Since(f.lastLog) > time.Minute {
		f.lastLog = time.Now()
		log.Printf("[ratelimit] %s: Redis indisponible, limite en memoire: %v", f.name, err)
	}
	f.mu.Unlock()
	return f.secondary.Allow(ctx, key)
}
