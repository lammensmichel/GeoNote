package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Port          string
	DatabaseURL   string
	JWTSecret     string
	JWTExpiry     time.Duration
	RedisURL      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	CacheTTL      time.Duration
	// TrustedIPHeader : en-tete ecrit par le proxy devant l'API et donnant
	// l'IP du client (ex. "X-Real-IP" sur Vercel). Vide = on garde RemoteAddr.
	TrustedIPHeader string
}

func Load() *Config {
	_ = godotenv.Load()

	expiry, err := time.ParseDuration(getEnv("JWT_EXPIRY", "24h"))
	if err != nil {
		expiry = 24 * time.Hour
	}

	cacheTTL, err2 := time.ParseDuration(getEnv("CACHE_TTL", "10s"))
	if err2 != nil {
		cacheTTL = 10 * time.Second
	}

	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))

	return &Config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://geonote:geonote@localhost:5432/geonote?sslmode=disable"),
		JWTSecret:       getEnv("JWT_SECRET", "dev-secret-change-me"),
		JWTExpiry:       expiry,
		RedisURL:        getEnv("REDIS_URL", ""),
		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:   getEnv("REDIS_PASSWORD", ""),
		RedisDB:         redisDB,
		CacheTTL:        cacheTTL,
		TrustedIPHeader: getEnv("TRUSTED_IP_HEADER", ""),
	}
}

// RedisOptions renvoie les options de connexion a Redis.
// REDIS_URL (ex. "rediss://default:motdepasse@hote:6379", format fourni par
// Upstash, TLS compris) est prioritaire sur REDIS_ADDR / REDIS_PASSWORD / REDIS_DB.
func (c *Config) RedisOptions() (*redis.Options, error) {
	if c.RedisURL != "" {
		opts, err := redis.ParseURL(c.RedisURL)
		if err != nil {
			// Ne pas inclure l'URL : elle contient le mot de passe.
			return nil, fmt.Errorf("REDIS_URL invalide")
		}
		return withFastTimeouts(opts), nil
	}
	return withFastTimeouts(&redis.Options{
		Addr:     c.RedisAddr,
		Password: c.RedisPassword,
		DB:       c.RedisDB,
	}), nil
}

// withFastTimeouts evite qu'un Redis lent bloque les requetes HTTP.
// Sans ContextTimeoutEnabled, go-redis ignore l'echeance du contexte et
// attend ReadTimeout (3 s par defaut) a chaque commande.
func withFastTimeouts(o *redis.Options) *redis.Options {
	o.ContextTimeoutEnabled = true
	o.DialTimeout = time.Second
	o.ReadTimeout = 500 * time.Millisecond
	o.WriteTimeout = 500 * time.Millisecond
	o.MaxRetries = 1
	return o
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
