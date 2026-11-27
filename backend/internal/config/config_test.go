package config

import (
	"strings"
	"testing"
	"time"
)

func TestRedisOptionsFromAddr(t *testing.T) {
	c := &Config{RedisAddr: "localhost:6379", RedisPassword: "pw", RedisDB: 2}
	o, err := c.RedisOptions()
	if err != nil {
		t.Fatal(err)
	}
	if o.Addr != "localhost:6379" || o.Password != "pw" || o.DB != 2 || o.TLSConfig != nil {
		t.Fatalf("options inattendues: %+v", o)
	}
}

func TestRedisOptionsURLWins(t *testing.T) {
	c := &Config{RedisURL: "rediss://default:secret@eu1-example.upstash.io:6379", RedisAddr: "localhost:6379"}
	o, err := c.RedisOptions()
	if err != nil {
		t.Fatal(err)
	}
	if o.Addr != "eu1-example.upstash.io:6379" || o.Password != "secret" || o.Username != "default" {
		t.Fatalf("options inattendues: %+v", o)
	}
	if o.TLSConfig == nil {
		t.Fatal("rediss:// doit activer TLS")
	}
}

func TestRedisOptionsBadURLHidesSecret(t *testing.T) {
	c := &Config{RedisURL: "http://user:topsecret@host"}
	_, err := c.RedisOptions()
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("le message d'erreur contient le mot de passe: %v", err)
	}
}

func TestRedisOptionsFastTimeouts(t *testing.T) {
	for _, c := range []*Config{
		{RedisAddr: "localhost:6379"},
		{RedisURL: "redis://localhost:6379/0"},
	} {
		o, err := c.RedisOptions()
		if err != nil {
			t.Fatal(err)
		}
		if !o.ContextTimeoutEnabled {
			t.Fatal("ContextTimeoutEnabled doit etre actif")
		}
		if o.ReadTimeout > time.Second || o.WriteTimeout > time.Second || o.DialTimeout > time.Second {
			t.Fatalf("timeouts trop longs: %+v", o)
		}
	}
}

// clearEnv neutralise les variables lues par Load pour un test isole.
func clearEnv(t *testing.T) {
	for _, k := range []string{"VERCEL", "APP_ENV", "DATABASE_URL", "JWT_SECRET", "DB_MAX_CONNS", "CORS_ALLOWED_ORIGINS"} {
		t.Setenv(k, "")
	}
}

const strongSecret = "0123456789abcdef0123456789abcdef-strong"

func TestLoadLocalDefaultsAreValid(t *testing.T) {
	clearEnv(t)
	c := Load()
	if c.Deployed {
		t.Fatal("ne doit pas etre en mode deploye sans VERCEL ni APP_ENV")
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("les valeurs de dev doivent passer en local: %v", err)
	}
	if c.DBMaxConns != defaultDBMaxConns {
		t.Fatalf("DBMaxConns = %d", c.DBMaxConns)
	}
	if !contains(c.CORSAllowedOrigins, "http://localhost:*") {
		t.Fatalf("localhost doit etre autorise en dev: %v", c.CORSAllowedOrigins)
	}
}

func TestDeployedRejectsDevValues(t *testing.T) {
	for _, env := range []map[string]string{
		{"VERCEL": "1"},
		{"APP_ENV": "production"},
		{"APP_ENV": "Production"},
		{"APP_ENV": "prod"},
	} {
		clearEnv(t)
		for k, v := range env {
			t.Setenv(k, v)
		}
		c := Load()
		if !c.Deployed {
			t.Fatalf("%v: doit etre en mode deploye", env)
		}
		if c.DBMaxConns != deployedDBMaxConns {
			t.Fatalf("%v: pool par defaut en production = %d", env, c.DBMaxConns)
		}
		err := c.Validate()
		if err == nil {
			t.Fatalf("%v: valeurs de dev acceptees en production", env)
		}
		if !strings.Contains(err.Error(), "JWT_SECRET") || !strings.Contains(err.Error(), "DATABASE_URL") {
			t.Fatalf("%v: erreur incomplete: %v", env, err)
		}
	}
}

func TestDeployedRejectsWeakSecrets(t *testing.T) {
	for _, s := range []string{"change-me-in-production", "dev-secret-change-me", "trop-court"} {
		clearEnv(t)
		t.Setenv("VERCEL", "1")
		t.Setenv("DATABASE_URL", "postgres://u:p@ep-x-pooler.eu-central-1.aws.neon.tech/db?sslmode=require")
		t.Setenv("JWT_SECRET", s)
		if err := Load().Validate(); err == nil {
			t.Fatalf("secret %q accepte en production", s)
		}
	}
}

func TestDeployedValidConfig(t *testing.T) {
	clearEnv(t)
	t.Setenv("VERCEL", "1")
	t.Setenv("DATABASE_URL", "postgres://u:p@ep-x-pooler.eu-central-1.aws.neon.tech/db?sslmode=require")
	t.Setenv("JWT_SECRET", strongSecret)
	t.Setenv("DB_MAX_CONNS", "3")
	t.Setenv("CORS_ALLOWED_ORIGINS", " https://app.example.be/ , https://example.be ,")
	c := Load()
	if err := c.Validate(); err != nil {
		t.Fatalf("config de production valide refusee: %v", err)
	}
	if c.DBMaxConns != 3 {
		t.Fatalf("DBMaxConns = %d", c.DBMaxConns)
	}
	want := []string{"https://app.example.be", "https://example.be"}
	if strings.Join(c.CORSAllowedOrigins, ",") != strings.Join(want, ",") {
		t.Fatalf("origines = %v, attendu %v (pas de localhost en production)", c.CORSAllowedOrigins, want)
	}
}

func TestCORSWildcardRejected(t *testing.T) {
	cases := []struct {
		deployed bool
		origin   string
	}{
		{false, "*"},
		{false, "https://*.example.be"},
		{true, "*"},
		{true, "http://localhost:*"},
	}
	for _, tc := range cases {
		c := &Config{Deployed: tc.deployed, JWTSecret: strongSecret, DatabaseURL: "postgres://x/y", CORSAllowedOrigins: []string{tc.origin}}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "CORS") {
			t.Fatalf("joker %q (deployed=%v) accepte: %v", tc.origin, tc.deployed, err)
		}
	}
}

func TestBadDBMaxConnsFallsBack(t *testing.T) {
	for _, v := range []string{"0", "-3", "abc"} {
		clearEnv(t)
		t.Setenv("DB_MAX_CONNS", v)
		if got := Load().DBMaxConns; got != defaultDBMaxConns {
			t.Fatalf("DB_MAX_CONNS=%q -> %d", v, got)
		}
	}
}

func TestPoolConfig(t *testing.T) {
	c := &Config{DatabaseURL: "postgres://u:p@localhost:5432/db?sslmode=disable", DBMaxConns: 5}
	pc, err := c.PoolConfig()
	if err != nil {
		t.Fatal(err)
	}
	if pc.MaxConns != 5 || pc.MinConns != 0 {
		t.Fatalf("MaxConns=%d MinConns=%d", pc.MaxConns, pc.MinConns)
	}
	if pc.MaxConnIdleTime <= 0 || pc.MaxConnIdleTime > 5*time.Minute {
		t.Fatalf("MaxConnIdleTime=%v", pc.MaxConnIdleTime)
	}
	if pc.MaxConnLifetime <= 0 || pc.ConnConfig.ConnectTimeout <= 0 {
		t.Fatalf("lifetime=%v connectTimeout=%v", pc.MaxConnLifetime, pc.ConnConfig.ConnectTimeout)
	}
}

func TestPoolConfigBadURLHidesSecret(t *testing.T) {
	c := &Config{DatabaseURL: "postgres://user:topsecret@host:notaport/db", DBMaxConns: 5}
	_, err := c.PoolConfig()
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("le message d'erreur contient le mot de passe: %v", err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
