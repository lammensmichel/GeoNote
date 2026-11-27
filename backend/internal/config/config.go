package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// Valeurs par defaut reservees au developpement local. En production
// (Deployed), Validate refuse de demarrer si elles sont encore utilisees.
const (
	defaultDatabaseURL = "postgres://geonote:geonote@localhost:5432/geonote?sslmode=disable"
	defaultJWTSecret   = "dev-secret-change-me"
	defaultDBMaxConns  = 10
	minJWTSecretLen    = 32
)

// Secrets d'exemple deja publies dans le depot : jamais acceptes en production.
var knownWeakSecrets = map[string]bool{
	defaultJWTSecret:          true,
	"change-me-in-production": true,
	"change-me":               true,
}

// Origines autorisees en developpement (appli Flutter web lancee en local).
var devCORSOrigins = []string{"http://localhost:*", "http://127.0.0.1:*"}

type Config struct {
	// Deployed est vrai sur Vercel (VERCEL=1) ou si APP_ENV=production.
	// Les valeurs de developpement y sont refusees (voir Validate).
	Deployed bool
	// DBMaxConns : connexions max du pool par instance (DB_MAX_CONNS).
	// Sur Vercel, chaque instance a son propre pool : garder une petite valeur.
	DBMaxConns int
	// CORSAllowedOrigins : origines du navigateur autorisees a appeler l'API
	// (appli web, page d'accueil). Localhost n'est ajoute qu'en developpement.
	CORSAllowedOrigins []string
	Port               string
	DatabaseURL        string
	JWTSecret          string
	JWTExpiry          time.Duration
	RedisURL           string
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	CacheTTL           time.Duration
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

	dbMaxConns, err3 := strconv.Atoi(getEnv("DB_MAX_CONNS", strconv.Itoa(defaultDBMaxConns)))
	if err3 != nil || dbMaxConns < 1 {
		dbMaxConns = defaultDBMaxConns
	}

	deployed := getEnv("VERCEL", "") != "" || strings.EqualFold(getEnv("APP_ENV", ""), "production")

	origins := splitList(getEnv("CORS_ALLOWED_ORIGINS", ""))
	if !deployed {
		origins = append(append([]string{}, devCORSOrigins...), origins...)
	}

	return &Config{
		Deployed:           deployed,
		DBMaxConns:         dbMaxConns,
		CORSAllowedOrigins: origins,
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        getEnv("DATABASE_URL", defaultDatabaseURL),
		JWTSecret:          getEnv("JWT_SECRET", defaultJWTSecret),
		JWTExpiry:          expiry,
		RedisURL:           getEnv("REDIS_URL", ""),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:      getEnv("REDIS_PASSWORD", ""),
		RedisDB:            redisDB,
		CacheTTL:           cacheTTL,
		TrustedIPHeader:    getEnv("TRUSTED_IP_HEADER", ""),
	}
}

// Validate verifie qu'aucune valeur de developpement n'est utilisee en
// production : un secret JWT connu permettrait a n'importe qui de fabriquer
// un jeton valide pour n'importe quel compte.
func (c *Config) Validate() error {
	var errs []error
	for _, o := range c.CORSAllowedOrigins {
		if strings.Contains(o, "*") && (c.Deployed || !isDevOrigin(o)) {
			errs = append(errs, fmt.Errorf("CORS_ALLOWED_ORIGINS: joker interdit (%q), lister les domaines exacts", o))
		}
	}
	if !c.Deployed {
		return errors.Join(errs...)
	}
	if knownWeakSecrets[c.JWTSecret] || len(c.JWTSecret) < minJWTSecretLen {
		errs = append(errs, fmt.Errorf("JWT_SECRET absent ou trop faible (%d caracteres minimum, aleatoire)", minJWTSecretLen))
	}
	if c.DatabaseURL == defaultDatabaseURL {
		errs = append(errs, errors.New("DATABASE_URL absente (URL Neon attendue)"))
	}
	return errors.Join(errs...)
}

// PoolConfig renvoie la configuration du pool PostgreSQL.
// Reglages pensés pour Vercel + Neon : peu de connexions par instance, aucune
// connexion gardee au repos (Neon peut alors se mettre en veille), et des
// connexions renouvelees regulierement.
func (c *Config) PoolConfig() (*pgxpool.Config, error) {
	pc, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		// Ne pas inclure l'URL : elle contient le mot de passe.
		return nil, errors.New("DATABASE_URL invalide")
	}
	pc.MaxConns = int32(c.DBMaxConns)
	pc.MinConns = 0
	pc.MaxConnIdleTime = time.Minute
	pc.MaxConnLifetime = 30 * time.Minute
	pc.HealthCheckPeriod = 30 * time.Second
	pc.ConnConfig.ConnectTimeout = 10 * time.Second
	return pc, nil
}

func isDevOrigin(o string) bool {
	for _, d := range devCORSOrigins {
		if o == d {
			return true
		}
	}
	return false
}

// splitList decoupe une liste separee par des virgules, sans elements vides.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.TrimSuffix(p, "/"))
		}
	}
	return out
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
