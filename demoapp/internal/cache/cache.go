// Package cache is a read-through JSON cache in Redis.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/opsschool/emulator/demoapp/internal/metrics"
)

// Cache wraps a Redis client.
type Cache struct {
	Client *redis.Client
	TTL    time.Duration
	Log    *slog.Logger
}

// New connects to Redis at addr.
func New(addr string, ttl time.Duration, log *slog.Logger) *Cache {
	return &Cache{
		Client: redis.NewClient(&redis.Options{
			Addr:         addr,
			DialTimeout:  500 * time.Millisecond,
			ReadTimeout:  500 * time.Millisecond,
			WriteTimeout: 500 * time.Millisecond,
			MaxRetries:   -1, // the shop falls back to MySQL instead
		}),
		TTL: ttl,
		Log: log,
	}
}

// Get returns the cached value for key, or calls load and caches its result.
// Redis errors fall through to load, so the shop keeps working without Redis.
func Get[T any](ctx context.Context, c *Cache, key string, load func() (T, error)) (T, error) {
	b, err := c.Client.Get(ctx, key).Bytes()
	switch {
	case err == nil:
		var v T
		if json.Unmarshal(b, &v) == nil {
			metrics.CacheRequests.WithLabelValues("hit").Inc()
			return v, nil
		}
		metrics.CacheRequests.WithLabelValues("error").Inc()
	case errors.Is(err, redis.Nil):
		metrics.CacheRequests.WithLabelValues("miss").Inc()
	default:
		metrics.CacheRequests.WithLabelValues("error").Inc()
		c.Log.Warn("cache get failed", "key", key, "err", err)
	}
	v, err := load()
	if err != nil {
		return v, err
	}
	if b, err := json.Marshal(v); err == nil {
		if err := c.Client.Set(ctx, key, b, c.TTL).Err(); err != nil {
			c.Log.Warn("cache set failed", "key", key, "err", err)
		}
	}
	return v, nil
}

// ErrLocked means another request holds the lock.
var ErrLocked = errors.New("locked")

// Lock takes a short-lived lock on key, so the same customer can't run two
// checkouts at once (a double-clicked "Place order" would charge twice). It
// fails closed: if Redis can't take the lock, the caller must not proceed.
func (c *Cache) Lock(ctx context.Context, key string, ttl time.Duration) (release func(), err error) {
	ok, err := c.Client.SetNX(ctx, "lock:"+key, "1", ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLocked
	}
	return func() { c.Client.Del(context.WithoutCancel(ctx), "lock:"+key) }, nil
}
