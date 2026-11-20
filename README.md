# Cache service

A small Go module that connects to Redis. It gives one `cache.Cache` interface over two backends, **Redis** and **in-memory**, and a Redis client (`redis.Client`, on go-redis v9) for features that are specific to Redis. It owns the Redis connection settings: other programs load them through this module and do not define their own.

Module path: `tw.com.championtek.cache`. Needs Go 1.26.

## What is in it

| Package | What it does |
|---|---|
| `cache` | The `Cache` interface (`Get`, `Set`, `Delete`, `Ping`, `Close`), `New(ctx, Config)` and `FromEnv(ctx)` |
| `redis` | `redis.Client` with options (address, auth, DB, pool, timeouts, TLS), plus `FromEnv(ctx)` and `OptionsFromEnv()` |
| `inmemory` | Process-local map with TTL, swept in the background |

## Configure

All settings come from environment variables. Unset or empty keeps the default.

| Variable | Meaning | Default |
|---|---|---|
| `REDIS_ADDR` | `host:port` | `127.0.0.1:6379` |
| `REDIS_USERNAME`, `REDIS_PASSWORD` | ACL user and password | empty |
| `REDIS_DB` | database number, 0 or more | `0` |
| `CACHE_BACKEND` | read only by `cache.FromEnv`: `inmemory` or `redis` | `inmemory` |

Pool size, timeouts and TLS are not read from the environment. Set them in code with the options `redis.WithPoolSize`, `WithReadTimeout`, `WithWriteTimeout`, `WithEnableTls`, `WithCACertPath` and `WithServerName`.

Values are trimmed. A bad `REDIS_DB` is reported by name, and no error contains the password. An empty `REDIS_PASSWORD` means no authentication. `cache.FromEnv` reads the `REDIS_*` variables only when `CACHE_BACKEND=redis`.

## Use

**As a cache (feature flag).** `cache.FromEnv` picks the backend from `CACHE_BACKEND`:

```go
c, err := cache.FromEnv(ctx)
// or choose in code:
c, err = cache.New(ctx, cache.Config{Backend: cache.BackendRedis, Redis: []redis.Option{redis.WithAddr("redis:6379")}})

err = c.Set(ctx, "key", "value", time.Hour) // ttl 0 means no expiry
v, err := c.Get(ctx, "key")                  // cache.ErrNotFound on a miss, for both backends
```

**As a Redis client.** `redis.FromEnv` always uses Redis and ignores `CACHE_BACKEND`. `Raw()` returns the go-redis client, which has transactions (`TxPipeline`, `Watch`) and any command through `Do`:

```go
c, err := redis.FromEnv(ctx)   // connects and pings; returns an error if Redis is unreachable
defer c.Close()
rdb := c.Raw()                  // *redis.Client from github.com/redis/go-redis/v9
err = rdb.Do(ctx, "WAITAOF", 1, 0, 100).Err()
```

Hashes, sets, pipelines and scan helpers are on `redis.Client` too. `Client.Pipe()` is one shared pipeliner and is not safe for concurrent use. Use `Raw().Pipeline()` per caller instead.

## Tests

```
go test -race -cover ./...                                  # unit tests (Redis through miniredis), 100% coverage
docker compose up -d redis && go test -tags integration ./redis/tests   # against a real Redis
```

`go.mod` is vendored locally (`vendor/` is not committed). Run `go mod vendor` after changing dependencies.

## Architecture
![Level Memory](level_memory.png)
