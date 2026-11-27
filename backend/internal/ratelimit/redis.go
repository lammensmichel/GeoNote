package ratelimit

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisTimeout borne chaque appel a Redis : une requete HTTP ne doit pas
// attendre un Redis lent, elle passe alors par la limite en memoire.
const redisTimeout = 300 * time.Millisecond

// tokenBucketScript applique le seau a jetons de facon atomique.
// KEYS[1] = cle ; ARGV = rate (jetons/s), burst, now (ms), ttl (ms).
// L'heure vient de l'appelant (pas de TIME dans le script) pour rester
// compatible avec Upstash et la replication des scripts.
var tokenBucketScript = redis.NewScript(`
local rate  = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now   = tonumber(ARGV[3])
local ttl   = tonumber(ARGV[4])

local data   = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts     = tonumber(data[2])
if tokens == nil or ts == nil then
  tokens = burst
  ts = now
end

local elapsed = (now - ts) / 1000
if elapsed > 0 then
  tokens = math.min(burst, tokens + elapsed * rate)
  ts = now
end

local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end

redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', tostring(ts))
redis.call('PEXPIRE', KEYS[1], ttl)
return allowed
`)

// RedisTokenBucket est un seau a jetons partage entre instances via Redis.
type RedisTokenBucket struct {
	client redis.Scripter
	prefix string
	rate   float64
	burst  int
	ttl    time.Duration
	now    func() time.Time
}

func NewRedisTokenBucket(client redis.Scripter, prefix string, rate float64, burst int) *RedisTokenBucket {
	// La cle peut disparaitre une fois le seau de nouveau plein.
	refill := time.Duration(math.Ceil(float64(burst)/rate*1000)) * time.Millisecond
	return &RedisTokenBucket{
		client: client,
		prefix: prefix,
		rate:   rate,
		burst:  burst,
		ttl:    refill + time.Second,
		now:    time.Now,
	}
}

func (b *RedisTokenBucket) Allow(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	res, err := tokenBucketScript.Run(ctx, b.client, []string{b.prefix + key},
		strconv.FormatFloat(b.rate, 'f', -1, 64),
		b.burst,
		b.now().UnixMilli(),
		b.ttl.Milliseconds(),
	).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

// RedisCooldown autorise une action par cle toutes les cooldown, via
// SET NX PX : atomique et partage entre instances.
type RedisCooldown struct {
	client   redis.Cmdable
	prefix   string
	cooldown time.Duration
}

func NewRedisCooldown(client redis.Cmdable, prefix string, cooldown time.Duration) *RedisCooldown {
	return &RedisCooldown{client: client, prefix: prefix, cooldown: cooldown}
}

func (c *RedisCooldown) Allow(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	return c.client.SetNX(ctx, c.prefix+key, 1, c.cooldown).Result()
}
