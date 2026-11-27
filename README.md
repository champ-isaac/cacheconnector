# Cache service

A small Go module with one `cache.Cache` interface over several key/value backends: **Redis**, **Valkey**, **Memcached** and **in-memory**. One binary can hold every backend, and the program picks one at start-up from a YAML config file (or a DSN, or an environment variable). Changing the driver is a config edit and a restart, not a rebuild. A program that wants a smaller binary can import only the backends it needs. For Redis-specific features there is also `redis.Client` (go-redis v9). The module owns the connection settings: other programs load them through it and do not define their own.

Module path: `github.com/champ-isaac/cacheconnector`. Needs Go 1.26.

## What is in it

| Package | What it does |
|---|---|
| `cache` (root) | The `Cache` interface (`Get`, `Set`, `Delete`, `Ping`, `Close`), `ErrNotFound`, and the registry: `FromFile(ctx, path)`, `ReadConfig(path)`, `FromConfig(ctx, Config)`, `Config.DriverName()`, `Open(ctx, dsn)`, `FromEnv(ctx)`, `Register`, `Backends` |
| `inmemory` | Process-local map with TTL, swept in the background. Scheme `inmemory` |
| `redis` | Scheme `redis` / `rediss` (TLS). Also `redis.Client` with options, `FromEnv` and `OptionsFromEnv` |
| `valkey` | Scheme `valkey` / `valkeys` (TLS). Valkey speaks the Redis protocol, so it reuses the Redis client |
| `memcached` | Scheme `memcached`, on gomemcache |
| `all` | Blank-imports every backend above. Import it to get all drivers in one binary |
| `cachetest` | The behaviour every backend must have, run from each backend's tests |

The root package has no backend dependency. Each backend registers itself in `init`, like a `database/sql` driver. Import `all` to compile in every driver and choose one at run time:

```go
import (
    "github.com/champ-isaac/cacheconnector"
    _ "github.com/champ-isaac/cacheconnector/all"   // or just one: .../redis, .../valkey, ...
)
```

## Choosing the driver from a config file

The usual way to use the module. Compile with `all`, name the driver in a YAML file, and read it at start-up:

```yaml
# cache.yaml
driver: redis                          # inmemory | redis | valkey | memcached
url: "redis://:secret@10.0.0.5:6379/0"  # optional DSN. Its scheme must agree with driver
```

Set `driver`, `url`, or both. A file that sets neither is an error.

```go
c, err := cache.FromFile(ctx, "cache.yaml")
```

To use another driver, change `driver:` and restart the process. Nothing is recompiled, because every driver is already in the binary and `FromFile` only looks the name up in the registry.

- With only `driver:`, the backend reads its own settings from its environment variables (`REDIS_ADDR`, `MEMCACHED_ADDRS`, ...). With `url:`, the DSN carries them (see DSNs below).
- **Neither `driver` nor `url` is an error.** An empty file, a file with only comments, and blank values all fail with `cache: config <path> sets neither driver nor url`. Nothing silently falls back to `inmemory`.
- **`driver` and `url` must agree.** If both are set, the `url` scheme must be the driver name or its TLS variant. Each backend declares its own TLS pair when it registers (see "Adding a backend"): `redis` declares `rediss` and `valkey` declares `valkeys`, so `driver: redis` accepts `redis://` and `rediss://`, but a typo such as `driver: redi` with `url: redis://…` is an error. Anything else is an error too, for example `driver: redis` with `url: memcached://host:11211` fails with `cache: driver "redis" does not match the url scheme "memcached"`. The error never prints the URL.
- **One YAML document only.** A file with a second document that has content (after a `---` line) is rejected with `cache: config <path> has more than one YAML document`, so the second one is never ignored silently. Empty documents (nothing in them, or only a comment) are skipped wherever they are, so a stray `---` before the config or after it is accepted. A file that has only empty documents fails with the neither-`driver`-nor-`url` error above. A syntax error in a later document is reported as a syntax error (see below).
- Unknown keys are an error, so a typo such as `drivr:` fails at start-up instead of silently selecting a default. The error does not name the key (see below).
- **Unknown driver error.** A name that is not registered fails with this text (the list is what the `all` package registers):

  ```
  cache: unknown backend "nope" (registered: inmemory, memcached, redis, rediss, valkey, valkeys); did you import its package?
  ```

  It names the driver you wrote and lists the registered drivers. It never echoes a DSN or URL: a value that is not a plain name, such as a DSN put in `driver:` by mistake, is shown as `(not a valid name)`, so a password in it does not reach logs. A DSN in `url:` with an unknown scheme is reported by its scheme only.
- **Quote the DSN.** Some DSNs are not valid as plain YAML. For example `url: inmemory:` fails to parse with a YAML syntax error, so write `url: "inmemory:"`.
- **YAML errors name the file and the stage, never the parser's message**, because that message can quote text from the file, such as a password. The two forms are `cache: parse config <path>: YAML syntax error` (the file is not valid YAML) and:

  ```
  cache: parse config <path>: invalid YAML content (expected one mapping with only the keys "driver" and "url", each set once, and text values)
  ```

  The second form is valid YAML that this config does not accept. One message covers every such case: an unknown key (such as `drivr:`), a duplicate key (`driver:` twice), a document that is not a mapping (a plain scalar such as `inmemory`, or a list), and a value that is not text (such as `driver: [redis]`). To find the problem, check the file yourself; the error gives no line number.
- If your program already has its own config struct, embed `cache.Config` (it has the same `driver` and `url` yaml keys) and call `cache.FromConfig(ctx, cfg.Cache)`. The file is then read by your loader, not by this module. `FromConfig` checks that `driver` and `url` agree, as above. Only `FromFile` rejects a config that sets neither; `FromConfig` with both empty opens `inmemory`.
- The driver is chosen once, when the program starts. Changing the file while the process runs has no effect.
- The tradeoff: the binary contains every client library (go-redis, gomemcache), so it is bigger than one that imports a single backend.
- **Which driver is running.** `Cache` has no name method, and `%T` cannot tell `redis` from `valkey` (both print `redis.adapter`). Read the resolved driver from the config instead. `Config.DriverName()` returns the backend name that `FromConfig` opens: the `url` scheme if `url` is set, else `driver`, else `inmemory`, in lower case. `FromFile` is `ReadConfig` followed by `FromConfig`, so to log the driver, call those two yourself:

  ```go
  cfg, err := cache.ReadConfig("cache.yaml")   // reads and checks the file, opens nothing
  if err != nil { ... }
  log.Printf("cache driver: %s", cfg.DriverName())
  c, err := cache.FromConfig(ctx, cfg)         // same as cache.FromFile(ctx, "cache.yaml")
  ```

  With an embedded `cache.Config`, call `cfg.Cache.DriverName()` the same way. This is better than logging `cfg.Driver`, which is empty when the default applies and is overridden by `url`. `DriverName` does not check that the backend is registered or that `url` and `driver` agree; `FromConfig` does that. It returns `""` when the value is not a plain name or `url` does not parse, so a DSN never ends up in a log line. The `Cache` interface is unchanged, and the value returned by `FromFile` and `FromConfig` is the backend's own type, not a wrapper.
- `FromEnv` does not use the agreement check: `CACHE_URL` still wins over `CACHE_BACKEND` (see Environment).

### Before you switch drivers

- **Switching is a data migration, not a toggle.** In a rolling restart, replicas on `inmemory` and replicas on `redis` do not share deletes, so reads can be stale for up to the TTL. The new driver also starts empty, so the origin of the data can see a burst of load, and this module has no protection against that. Try a rolling restart in staging first.
- **The drivers are not fully interchangeable.** Memcached rejects keys longer than 250 bytes or with spaces or control characters (`memcache.ErrMalformedKey`), and it rounds a TTL up to whole seconds. A switch to memcached can start fine and then fail at the first `Set` with such a key, not at start-up. Test the switch in staging. More in "Backend differences" below.

## Use

```go
c, err := cache.Open(ctx, "redis://:secret@127.0.0.1:6379/0")
// or let a config file decide (see above), or the environment:
c, err = cache.FromFile(ctx, "cache.yaml")
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

`cache.FromEnv` is the same lookup with the values taken from the environment instead of a file. It opens `CACHE_URL` if it is set. Otherwise it opens the backend named by `CACHE_BACKEND` (default `inmemory`), and that backend reads its own variables. Unset or empty keeps the default.

| Variable | Meaning | Default |
|---|---|---|
| `CACHE_URL` | a DSN; wins over `CACHE_BACKEND` | none |
| `CACHE_BACKEND` | `inmemory`, `redis`, `valkey` or `memcached` | `inmemory` |
| `REDIS_ADDR`, `REDIS_USERNAME`, `REDIS_PASSWORD`, `REDIS_DB` | Redis settings (`VALKEY_*` for Valkey) | `127.0.0.1:6379`, empty, empty, `0` |
| `MEMCACHED_ADDRS` | comma separated `host:port` list | `127.0.0.1:11211` |

Values are trimmed. A bad `REDIS_DB` is reported by name, and an empty password means no authentication. Pool size, timeouts and TLS are not read from the environment: use a DSN, or the options `redis.WithPoolSize`, `WithReadTimeout`, `WithWriteTimeout`, `WithEnableTLS`, `WithCACertPath` and `WithServerName`.

### Backend differences

- **Memcached** counts TTL in whole seconds, so a ttl is rounded up (an entry can still live up to one second shorter than asked). Memcached stores the expiry as a 32-bit Unix time, so a ttl that would end after 19 January 2038 (Unix time 2147483647, 03:14:07 UTC) is clamped to that moment. Keys are at most 250 bytes with no spaces or control characters (`memcache.ErrMalformedKey`), and the empty key is rejected too: the server refuses it, and `Get`, `Set` and `Delete` return an error that is neither `cache.ErrNotFound` nor `memcache.ErrMalformedKey`. Valkey, Redis and in-memory accept the empty key like any other. The client has no context support: `ctx` is checked before each call, so a deadline that passes during a call does not cut it short, and `timeout` bounds a call.
- **Valkey and Redis** honour a call's context deadline. A call stuck on a slow or silent server returns an error that wraps `context.DeadlineExceeded` when the deadline passes, instead of waiting for the client's read timeout. Memcached does not do this (see above).
- **In-memory** is per process. It is for tests, local development and single-instance use, not for sharing between replicas.
- Anything beyond the five methods, such as Redis hashes, pipelines and transactions, is on `redis.Client`, below.

### Adding a backend

Write a type that implements `cache.Cache` and register a factory in `init`:

```go
func init() { cache.Register("mystore", func(ctx context.Context, u *url.URL) (cache.Cache, error) { ... }) }
```

If the backend also has a TLS scheme, register that scheme too and name it with `cache.WithTLS` on the plain one:

```go
func init() {
    cache.Register("mystore", factory, cache.WithTLS("mystores")) // declares the pair
    cache.Register("mystores", tlsFactory)                        // the TLS scheme still needs its own Register
}
```

`WithTLS` only records that `mystores` is the TLS variant of `mystore`. With it, a config file with `driver: mystore` accepts `url: "mystores://…"` as well as `url: "mystore://…"`. Without it, a driver and a `url` with a different scheme are an error, because `driver` and `url` must agree. `Register` panics, and leaves the registry unchanged, if the `WithTLS` scheme is empty or the same as the scheme, or if the scheme is registered twice. A call with no option works as before. The `redis` and `valkey` packages declare their pairs this way (`cache.WithTLS("rediss")` and `cache.WithTLS("valkeys")`). The root package holds no TLS pair of its own. (It still names `inmemory`, as the default when nothing else is set.)

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
docker compose up -d redis && go test -tags integration ./redis/tests   # against the redis container on 127.0.0.1:6381
docker compose up -d valkey && go test -tags integration ./valkey/tests   # against the valkey container on 127.0.0.1:6380
docker compose up -d memcached && go test -tags integration ./memcached/tests   # against the memcached container on 127.0.0.1:11211
MEMCACHED_TEST_FLUSH=1 go test -tags integration ./memcached/tests   # same, when the memcached server already holds data (see below)
```

The valkey tests connect with password `user` and use database 1, which they flush before and after the run, so data in other databases is left alone. Both new packages also run the shared `cachetest` contract, and its TTL cases sleep for real.

Memcached has no databases, so the memcached tests flush the whole server before and after the run. To protect data they do not own, they first count the live items on `127.0.0.1:11211` and refuse to run if there are any, unless `MEMCACHED_TEST_FLUSH=1` is set. On an empty server (a fresh `memcached` container) the variable is not needed. If the server holds data, the tests need `MEMCACHED_TEST_FLUSH=1` to run, and it will be wiped. Run them only against the compose container.

Redis and Valkey run against miniredis, and Memcached against an in-process fake server, so the unit tests need no Docker. `-coverpkg=./...` counts the shared `cachetest` package, which the backends exercise.

`go.mod` is vendored locally (`vendor/` is not committed). Run `go mod vendor` after changing dependencies.

### Docker compose services

`docker-compose.yaml` starts a real server for each backend that needs one. `docker compose up -d` starts all of them; the unit tests above do not need any.

| Service | Image | Host port → container port | Authentication | Settings for this module |
|---|---|---|---|---|
| `redis` | `redis:latest` | `6381` → `6379` | `--requirepass user` | the integration tests connect to `127.0.0.1:6381` as user `default` with password `user` |
| `valkey` | `valkey/valkey:latest` | `6380` → `6379` | `--requirepass user` | `VALKEY_ADDR=127.0.0.1:6380 VALKEY_PASSWORD=user`, or the DSN `valkey://:user@127.0.0.1:6380/0` |
| `memcached` | `memcached:latest` | `11211` → `11211` | none | `MEMCACHED_ADDRS=127.0.0.1:11211`, or the DSN `memcached://127.0.0.1:11211` |
| `redis-ui` | `redis/redisinsight:latest` | `5540` → `5540` | n/a | RedisInsight web UI. It reaches `redis` with `RI_REDIS_HOST=redis`, `RI_REDIS_PORT=6379`, `RI_REDIS_USERNAME=default` and `RI_REDIS_PASSWORD=user` |

- **Why `redis` is on host port 6381.** Host port 6379 is held by Colima's SSH forwarder for another running container, so the `redis` service is published on host port 6381 and container port 6379. Inside the compose network `redis` is still on 6379, which is why `redis-ui` uses `RI_REDIS_PORT=6379`. The Redis integration test uses `127.0.0.1:6381` for this reason.
- **Why `valkey` is on host port 6380.** The container listens on 6379, and the host port is 6380, next to `redis` on 6381.
- **`memcached`** has no authentication. 11211 is the default the memcached backend uses.
- **`inmemory`** runs inside the Go process, so it has no container.

## Benchmarks

[Benchmark.md](Benchmark.md) reports a five-round comparison of the backends: method, environment, one table per operation, and a summary. To run one round against the compose services:

```
docker compose up -d redis valkey memcached
go test -tags integration -p 1 -run '^$' -bench . -benchmem -count=1 ./...   # one round; repeat for more rounds
```

The memcached benchmarks flush the whole server, with the same guard as the memcached tests above.

## Architecture

How the packages fit together. Backends depend on the root package, never the other way round, so the root stays free of backend code and a program can import one backend or all of them. With `all`, every backend is in the binary and the config file picks one by name when the program starts.

```mermaid
flowchart TB
    app["Your program"]

    subgraph core["cache (root package, no backend imports)"]
        iface["Cache interface<br/>Get / Set / Delete / Ping / Close"]
        reg["Registry<br/>FromFile / FromConfig / Open / FromEnv<br/>Register / Backends"]
    end

    subgraph backends["Backends: each registers its DSN scheme in init()"]
        mem["inmemory<br/>inmemory:"]
        redis["redis<br/>redis:// and rediss://<br/>also redis.Client (go-redis)"]
        valkey["valkey<br/>valkey:// and valkeys://"]
        mc["memcached<br/>memcached://"]
    end

    all["all<br/>blank-imports every backend"]
    ct["cachetest<br/>shared contract suite"]
    ring["utils.HashRing<br/>consistent hashing, standalone"]

    heap[("process memory")]
    rs[("Redis server")]
    vs[("Valkey server")]
    ms[("Memcached server")]

    cfg[/"cache.yaml<br/>driver: redis"/]
    cfg -->|"read at start-up"| reg
    app -->|"cache.FromFile(path), Open(dsn) or FromEnv()"| reg
    app -.->|"or import _ all"| all
    all --> backends
    backends -->|"Register(scheme, factory)"| reg
    backends -->|"implement"| iface
    valkey -->|"reuses client"| redis
    ct -.->|"run by each backend's tests"| backends

    mem --> heap
    redis --> rs
    valkey --> vs
    mc --> ms
```
