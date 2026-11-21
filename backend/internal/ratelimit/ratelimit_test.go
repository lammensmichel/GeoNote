package ratelimit

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

// fakeClock permet d'avancer le temps dans les tests sans dormir.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *fakeClock               { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }
func mustAllow(t *testing.T, l Limiter, key string, want bool) {
	t.Helper()
	got, err := l.Allow(ctx, key)
	if err != nil {
		t.Fatalf("Allow(%q) erreur: %v", key, err)
	}
	if got != want {
		t.Fatalf("Allow(%q) = %v, attendu %v", key, got, want)
	}
}

// ---- Memoire ----

func TestMemoryTokenBucketBurstThenRefill(t *testing.T) {
	clk := newClock()
	b := NewMemoryTokenBucket(2, 3) // 2 jetons/s, pic 3
	b.now = clk.now

	for i := 0; i < 3; i++ {
		mustAllow(t, b, "ip1", true)
	}
	mustAllow(t, b, "ip1", false)
	mustAllow(t, b, "ip2", true) // autre cle, autre seau

	clk.add(500 * time.Millisecond) // +1 jeton
	mustAllow(t, b, "ip1", true)
	mustAllow(t, b, "ip1", false)

	clk.add(time.Hour) // plafonne a burst
	for i := 0; i < 3; i++ {
		mustAllow(t, b, "ip1", true)
	}
	mustAllow(t, b, "ip1", false)
}

func TestMemoryTokenBucketSweep(t *testing.T) {
	clk := newClock()
	b := NewMemoryTokenBucket(1, 1)
	b.now = clk.now
	mustAllow(t, b, "old", true)
	clk.add(2 * sweepEvery)
	mustAllow(t, b, "new", true)
	if _, ok := b.buckets["old"]; ok {
		t.Fatal("le seau inactif aurait du etre supprime")
	}
}

func TestMemoryCooldown(t *testing.T) {
	clk := newClock()
	c := NewMemoryCooldown(5 * time.Second)
	c.now = clk.now

	mustAllow(t, c, "u1", true)
	mustAllow(t, c, "u1", false)
	mustAllow(t, c, "u2", true)

	clk.add(4 * time.Second)
	mustAllow(t, c, "u1", false) // refus : ne repousse pas le delai
	clk.add(time.Second)
	mustAllow(t, c, "u1", true)
}

func TestMemoryConcurrent(t *testing.T) {
	b := NewMemoryTokenBucket(0.001, 10)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := b.Allow(ctx, "k"); ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 10 {
		t.Fatalf("%d requetes acceptees, attendu 10", got)
	}
}

// ---- Repli ----

type failing struct{ calls int }

func (f *failing) Allow(context.Context, string) (bool, error) {
	f.calls++
	return false, errors.New("redis: connection refused")
}

func TestFallbackUsesSecondaryOnError(t *testing.T) {
	p := &failing{}
	l := WithFallback("test", p, NewMemoryCooldown(time.Minute))
	mustAllow(t, l, "u", true)
	mustAllow(t, l, "u", false)
	if p.calls != 2 {
		t.Fatalf("primary appele %d fois, attendu 2", p.calls)
	}
}

func TestFallbackPrefersPrimary(t *testing.T) {
	sec := NewMemoryCooldown(time.Minute)
	l := WithFallback("test", NewMemoryTokenBucket(1, 5), sec)
	mustAllow(t, l, "u", true)
	if len(sec.last) != 0 {
		t.Fatal("secondary ne doit pas etre utilise quand primary repond")
	}
}

// ---- Redis ----
// Ces tests tournent si TEST_REDIS_ADDR est defini (la CI fournit un Redis).

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR non defini")
	}
	c := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	if err := c.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis %s injoignable: %v", addr, err)
	}
	if err := c.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FlushDB: %v", err)
	}
	t.Cleanup(func() { c.FlushDB(ctx); c.Close() })
	return c
}

func TestRedisTokenBucket(t *testing.T) {
	c := redisClient(t)
	clk := newClock()
	b := NewRedisTokenBucket(c, "test:rl:", 2, 3)
	b.now = clk.now

	for i := 0; i < 3; i++ {
		mustAllow(t, b, "ip1", true)
	}
	mustAllow(t, b, "ip1", false)
	mustAllow(t, b, "ip2", true)

	clk.add(500 * time.Millisecond)
	mustAllow(t, b, "ip1", true)
	mustAllow(t, b, "ip1", false)

	clk.add(-10 * time.Second) // horloge d'une autre instance en retard
	mustAllow(t, b, "ip1", false)

	clk.add(time.Hour)
	for i := 0; i < 3; i++ {
		mustAllow(t, b, "ip1", true)
	}
	mustAllow(t, b, "ip1", false)

	ttl := c.PTTL(ctx, "test:rl:ip1").Val()
	if ttl <= 0 || ttl > b.ttl {
		t.Fatalf("TTL de la cle = %v, attendu entre 0 et %v", ttl, b.ttl)
	}
}

// Deux instances (deux limiteurs) partagent le meme seau.
func TestRedisTokenBucketShared(t *testing.T) {
	c := redisClient(t)
	a := NewRedisTokenBucket(c, "test:rl:", 0.001, 4)
	b := NewRedisTokenBucket(c, "test:rl:", 0.001, 4)

	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		l := a
		if i%2 == 1 {
			l = b
		}
		go func() {
			defer wg.Done()
			ok, err := l.Allow(ctx, "shared")
			if err != nil {
				t.Errorf("Allow: %v", err)
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 4 {
		t.Fatalf("%d requetes acceptees au total, attendu 4", got)
	}
}

func TestRedisCooldown(t *testing.T) {
	c := redisClient(t)
	a := NewRedisCooldown(c, "test:spam:", 300*time.Millisecond)
	b := NewRedisCooldown(c, "test:spam:", 300*time.Millisecond)

	mustAllow(t, a, "u1", true)
	mustAllow(t, b, "u1", false) // autre instance : meme delai
	mustAllow(t, b, "u2", true)

	time.Sleep(400 * time.Millisecond)
	mustAllow(t, b, "u1", true)
}

func TestRedisErrorFallsBackToMemory(t *testing.T) {
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 50 * time.Millisecond})
	defer c.Close()
	l := WithFallback("test", NewRedisCooldown(c, "x:", time.Minute), NewMemoryCooldown(time.Minute))
	mustAllow(t, l, "u", true)
	mustAllow(t, l, "u", false)
}
