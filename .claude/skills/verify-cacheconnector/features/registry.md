# Choosing a backend

A program picks its backend by name at start-up, from a config file, a DSN or the environment. Importing `all` registers every backend.

## Sub-features

- `reg-list` `cache.Backends()` lists the registered schemes, sorted.
- `reg-file` `cache.FromFile` opens the backend a YAML file names; `Config.Resolved()` returns its name.
- `reg-unknown` An unknown scheme is an error that lists the registered backends.
- `reg-mismatch` A `driver` and a `url` whose schemes disagree is an error.
- `reg-default` An empty `Config` opens `inmemory`.
- `reg-typo` An unknown key in the YAML file is an error that does not echo the file.
- `reg-env` `FromEnv` with `CACHE_BACKEND=inmemory` opens `inmemory`.

## How to get to it (user POV)

- `cache.FromFile(ctx, "cache.yaml")`, `cache.Open(ctx, dsn)`, `cache.FromConfig(ctx, cfg)`, `cache.FromEnv(ctx)`.

## Driving it with verify.sh

Preconditions: baseline from the README.

- **Registry section.** `verify.sh drive`. The `registry` section of the transcript shows six `[PASS]` lines, each with the list or error the caller got, such as `(registered: inmemory, memcached, redis, rediss, valkey, valkeys)`.
- **Config files.** Each `<backend>.yaml` in the evidence directory is the file the backend was opened from.

## Gotchas

- `FromEnv` is not covered for `redis`, `valkey` or `memcached`, because those need `REDIS_ADDR` and similar variables. Add a journey if the environment path changes.
- `redis` and `valkey` also register `rediss` and `valkeys` (TLS). The driver does not open those, since compose has no TLS server.
