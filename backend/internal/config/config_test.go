package config

import (
	"strings"
	"testing"
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
