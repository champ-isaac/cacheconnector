# Cache service

One `cache.Cache` interface over two backends: **Redis** and **in-memory**.

## Feature flag
The backend is chosen at start-up. Set the environment variable and call `cache.FromEnv`:

| `CACHE_BACKEND` | Backend |
|---|---|
| `inmemory` (default) | process-local map with TTL, swept in the background |
| `redis` | Redis, configured by `REDIS_ADDR` (default `127.0.0.1:6379`), `REDIS_USERNAME`, `REDIS_PASSWORD`, `REDIS_DB` |

```go
c, err := cache.FromEnv(ctx)            // reads CACHE_BACKEND
// or choose in code:
c, err = cache.New(ctx, cache.Config{Backend: cache.BackendRedis, Redis: []redis.Option{redis.WithAddr("redis:6379")}})

err = c.Set(ctx, "key", "value", time.Hour) // ttl 0 means no expiry
v, err := c.Get(ctx, "key")                  // cache.ErrNotFound on a miss, for both backends
```
Backend specific features (hashes, sets, pipelines, scan) stay on `redis.Client`.

## Tests
```
go test -race -cover ./...                                  # unit tests (Redis through miniredis), 100% coverage
docker compose up -d redis && go test -tags integration ./redis/tests   # against a real Redis
```

## Architecture
![Level Memory](level_memory.png)
