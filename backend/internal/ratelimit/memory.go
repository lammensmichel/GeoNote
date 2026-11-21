package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Le nettoyage des entrees anciennes se fait pendant les appels, sans
// goroutine de fond (rien ne doit tourner entre deux requetes en serverless).
const sweepEvery = time.Minute

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// MemoryTokenBucket est un seau a jetons en memoire : burst jetons au maximum,
// recharge de rate jetons par seconde.
type MemoryTokenBucket struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

func NewMemoryTokenBucket(rate float64, burst int) *MemoryTokenBucket {
	return &MemoryTokenBucket{
		rate:    rate,
		burst:   float64(burst),
		now:     time.Now,
		buckets: make(map[string]*bucket),
	}
}

func (m *MemoryTokenBucket) Allow(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.sweep(now)

	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{tokens: m.burst, lastSeen: now}
		m.buckets[key] = b
	}

	elapsed := now.Sub(b.lastSeen).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * m.rate
		if b.tokens > m.burst {
			b.tokens = m.burst
		}
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false, nil
	}
	b.tokens--
	return true, nil
}

// sweep supprime les seaux pleins depuis longtemps (appelee avec mu tenu).
func (m *MemoryTokenBucket) sweep(now time.Time) {
	if now.Sub(m.lastSweep) < sweepEvery {
		return
	}
	m.lastSweep = now
	for k, b := range m.buckets {
		if now.Sub(b.lastSeen) > sweepEvery {
			delete(m.buckets, k)
		}
	}
}

// MemoryCooldown autorise une action par cle toutes les cooldown.
// Une tentative refusee ne repousse pas le delai.
type MemoryCooldown struct {
	cooldown time.Duration
	now      func() time.Time

	mu        sync.Mutex
	last      map[string]time.Time
	lastSweep time.Time
}

func NewMemoryCooldown(cooldown time.Duration) *MemoryCooldown {
	return &MemoryCooldown{
		cooldown: cooldown,
		now:      time.Now,
		last:     make(map[string]time.Time),
	}
}

func (m *MemoryCooldown) Allow(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if now.Sub(m.lastSweep) >= sweepEvery {
		m.lastSweep = now
		for k, t := range m.last {
			if now.Sub(t) >= m.cooldown {
				delete(m.last, k)
			}
		}
	}

	if t, ok := m.last[key]; ok && now.Sub(t) < m.cooldown {
		return false, nil
	}
	m.last[key] = now
	return true, nil
}
