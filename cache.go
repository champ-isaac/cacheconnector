// Package cache gives one Cache interface over two backends, Redis and in-memory.
// The backend is a feature flag: set CACHE_BACKEND=redis or CACHE_BACKEND=inmemory (see FromEnv),
// or fill Config.Backend yourself.
package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/champ-isaac/cacheconnector/inmemory"
	"github.com/champ-isaac/cacheconnector/redis"
)

// ErrNotFound is returned by Get when the key is missing or expired.
var ErrNotFound = errors.New("cache: key not found")

type Backend string

const (
	BackendInMemory Backend = "inmemory"
	BackendRedis    Backend = "redis"

	// EnvBackend is the name of the feature flag environment variable.
	EnvBackend = "CACHE_BACKEND"
)

// Cache is the part of a cache that both backends support. A ttl of zero means no expiry.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Ping(ctx context.Context) error
	Close() error
}

type Config struct {
	Backend  Backend // empty means in-memory
	Redis    []redis.Option
	InMemory []inmemory.Option
}

func New(ctx context.Context, cfg Config) (Cache, error) {
	switch cfg.Backend {
	case "", BackendInMemory:
		return memoryCache{inmemory.New(cfg.InMemory...)}, nil
	case BackendRedis:
		c, err := redis.New(ctx, cfg.Redis...)
		if err != nil {
			return nil, err
		}
		return redisCache{c}, nil
	default:
		return nil, fmt.Errorf("cache: unknown backend %q, want %q or %q", cfg.Backend, BackendInMemory, BackendRedis)
	}
}

// FromEnv builds a cache from environment variables: CACHE_BACKEND (inmemory,
// the default, or redis) and, for redis only, the settings that
// redis.OptionsFromEnv documents (REDIS_ADDR, REDIS_USERNAME, ...).
func FromEnv(ctx context.Context) (Cache, error) {
	cfg := Config{Backend: Backend(os.Getenv(EnvBackend))}
	if cfg.Backend == BackendRedis {
		opts, err := redis.OptionsFromEnv()
		if err != nil {
			return nil, fmt.Errorf("cache: %w", err)
		}
		cfg.Redis = opts
	}
	return New(ctx, cfg)
}

type memoryCache struct{ c *inmemory.Client }

func (m memoryCache) Get(_ context.Context, key string) (string, error) {
	if v, ok := m.c.Get(key); ok {
		return v, nil
	}
	return "", ErrNotFound
}

func (m memoryCache) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.c.Set(key, value, ttl)
	return nil
}

func (m memoryCache) Delete(_ context.Context, key string) error {
	m.c.Delete(key)
	return nil
}

func (m memoryCache) Ping(context.Context) error { return nil }
func (m memoryCache) Close() error               { return m.c.Close() }

type redisCache struct{ c *redis.Client }

func (r redisCache) Get(ctx context.Context, key string) (string, error) {
	v, err := r.c.Get(ctx, key)
	if errors.Is(err, goredis.Nil) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (r redisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.c.Set(ctx, key, value, ttl)
}

func (r redisCache) Delete(ctx context.Context, key string) error { return r.c.Delete(ctx, key) }
func (r redisCache) Ping(ctx context.Context) error               { return r.c.Raw().Ping(ctx).Err() }
func (r redisCache) Close() error                                 { return r.c.Close() }
