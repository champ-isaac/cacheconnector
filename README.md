# Cache service

A small Go module with one `cache.Cache` interface over several key/value backends: **Redis**, **Valkey**, **Memcached** and **in-memory**. A program picks the backend with a DSN or an environment variable, and only the backends it imports are compiled in. For Redis-specific features there is also `redis.Client` (go-redis v9). The module owns the connection settings: other programs load them through it and do not define their own.

Module path: `github.com/champ-isaac/cacheconnector`. Needs Go 1.26.

## What is in it

| Package | What it does |
|---|---|
| `cache` (root) | The `Cache` interface (`Get`, `Set`, `Delete`, `Ping`, `Close`), `ErrNotFound`, and the registry: `Open(ctx, dsn)`, `FromEnv(ctx)`, `Register`, `Backends` |
| `inmemory` | Process-local map with TTL, swept in the background. Scheme `inmemory` |
| `redis` | Scheme `redis` / `rediss` (TLS). Also `redis.Client` with options, `FromEnv` and `OptionsFromEnv` |
| `valkey` | Scheme `valkey` / `valkeys` (TLS). Valkey speaks the Redis protocol, so it reuses the Redis client |
| `memcached` | Scheme `memcached`, on gomemcache |
| `all` | Blank-imports every backend above |
| `cachetest` | The behaviour every backend must have, run from each backend's tests |

The root package has no backend dependency. Each backend registers itself in `init`, like a `database/sql` driver, so import the ones you use:

```go
import (
    "github.com/champ-isaac/cacheconnector"
    _ "github.com/champ-isaac/cacheconnector/redis"   // or valkey, memcached, inmemory, or all
)
```

## Use

```go
c, err := cache.Open(ctx, "redis://:secret@127.0.0.1:6379/0")
// or let the environment decide:
c, err = cache.FromEnv(ctx)
defer c.Close()

err = c.Set(ctx, "key", []byte("value"), time.Hour) // ttl 0 or less means no expiry
v, err := c.Get(ctx, "key")                         // cache.ErrNotFound on a miss, for every backend
err = c.Delete(ctx, "key")                          // deleting a missing key is not an error
```

Values are `[]byte`. A returned slice is yours to modify. Every backend passes the same tests (`cachetest`), so code written against `cache.Cache` behaves the same on all of them, with the exceptions below.

### DSNs

| Backend | DSN |
|---|---|
| in-memory | `inmemory:` or `inmemory:?cleanup_interval=30s` |
| Redis | `redis://[user]:[password]@host:6379/<db>` |
| Redis with TLS | `rediss://host:6379?ca_cert=/path/ca.pem&servername=redis.internal` (`servername` defaults to the host) |
| Valkey | `valkey://...` and `valkeys://...`, same format as Redis |
| Memcached | `memcached://host:11211?server=host2:11211&timeout=200ms&max_idle_conns=8` |

An error from `Open` never contains the DSN, because it can hold a password.

### Environment

`cache.FromEnv` opens `CACHE_URL` if it is set. Otherwise it opens the backend named by `CACHE_BACKEND` (default `inmemory`), and that backend reads its own variables. Unset or empty keeps the default.

| Variable | Meaning | Default |
|---|---|---|
| `CACHE_URL` | a DSN; wins over `CACHE_BACKEND` | none |
| `CACHE_BACKEND` | `inmemory`, `redis`, `valkey` or `memcached` | `inmemory` |
| `REDIS_ADDR`, `REDIS_USERNAME`, `REDIS_PASSWORD`, `REDIS_DB` | Redis settings (`VALKEY_*` for Valkey) | `127.0.0.1:6379`, empty, empty, `0` |
| `MEMCACHED_ADDRS` | comma separated `host:port` list | `127.0.0.1:11211` |

Values are trimmed. A bad `REDIS_DB` is reported by name, and an empty password means no authentication. Pool size, timeouts and TLS are not read from the environment: use a DSN, or the options `redis.WithPoolSize`, `WithReadTimeout`, `WithWriteTimeout`, `WithEnableTls`, `WithCACertPath` and `WithServerName`.

### Backend differences

- **Memcached** counts TTL in whole seconds, so a ttl is rounded up (an entry never lives shorter than asked). Keys are at most 250 bytes with no spaces or control characters. The client has no context support: `ctx` is checked before each call and `timeout` bounds a call.
- **In-memory** is per process. It is for tests, local development and single-instance use, not for sharing between replicas.
- Anything beyond the five methods, such as Redis hashes, pipelines and transactions, is on `redis.Client`, below.

### Adding a backend

Write a type that implements `cache.Cache` and register a factory in `init`:

```go
func init() { cache.Register("mystore", func(ctx context.Context, u *url.URL) (cache.Cache, error) { ... }) }
```

Then run it through `cachetest.Run(t, newCache, advance)` in its tests. That is what keeps the backends interchangeable.

### As a Redis client

`redis.FromEnv` always uses Redis and ignores `CACHE_BACKEND`. `Raw()` returns the go-redis client, which has transactions (`TxPipeline`, `Watch`) and any command through `Do`:

```go
c, err := redis.FromEnv(ctx)   // connects and pings; returns an error if Redis is unreachable
defer c.Close()
rdb := c.Raw()                  // *redis.Client from github.com/redis/go-redis/v9
err = rdb.Do(ctx, "WAITAOF", 1, 0, 100).Err()
```

Hashes, sets, pipelines and scan helpers are on `redis.Client` too. `Client.Pipe()` is one shared pipeliner and is not safe for concurrent use. Use `Raw().Pipeline()` per caller instead.

## Tests

```
go test -race -coverpkg=./... -cover ./...                   # unit tests, 100% coverage
docker compose up -d redis && go test -tags integration ./redis/tests   # against a real Redis
```

Redis and Valkey run against miniredis, and Memcached against an in-process fake server, so the unit tests need no Docker. `-coverpkg=./...` counts the shared `cachetest` package, which the backends exercise.

`go.mod` is vendored locally (`vendor/` is not committed). Run `go mod vendor` after changing dependencies.

## Architecture
![Level Memory](level_memory.png)
