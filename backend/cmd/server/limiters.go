package main

import (
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mimi6060/GeoNote/backend/internal/ratelimit"
)

const (
	ipRate       = 10              // requetes par seconde et par IP
	ipBurst      = 20              // pic autorise
	postCooldown = 5 * time.Second // delai entre deux messages d'un meme utilisateur
)

// newLimiters construit le limiteur par IP et l'anti-spam de creation de
// message. Avec Redis, la limite est commune a toutes les instances et
// retombe en memoire si Redis tombe ; sans Redis, elle est en memoire.
func newLimiters(client *redis.Client) (ip, post ratelimit.Limiter) {
	memIP := ratelimit.NewMemoryTokenBucket(ipRate, ipBurst)
	memPost := ratelimit.NewMemoryCooldown(postCooldown)
	if client == nil {
		return memIP, memPost
	}
	ip = ratelimit.WithFallback("rate-limit",
		ratelimit.NewRedisTokenBucket(client, "rl:ip:", ipRate, ipBurst), memIP)
	post = ratelimit.WithFallback("anti-spam",
		ratelimit.NewRedisCooldown(client, "spam:post:", postCooldown), memPost)
	return ip, post
}
