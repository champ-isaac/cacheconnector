# cacheconnector verification map

The maintained source for verifying what a consumer of cacheconnector sees. Read this index, then the matching feature file.

## Baseline preconditions

- Run from the repo root with Docker running.
- `verify.sh up` has started `redis` (127.0.0.1:6381), `valkey` (127.0.0.1:6380) and `memcached` (127.0.0.1:11211) from `docker-compose.yaml`. `inmemory` needs nothing.
- `verify.sh doctor` passes for all four backends.
- Never flush a server. Use only keys with the prefix `verify-<run-id>:`.

## Driving conventions

- Use only the public API: `cache.FromFile`, `Open`, `FromConfig`, `FromEnv`, `Backends`, `Config.Resolved`, and the `Cache` methods.
- Open each backend from a YAML config file, the way a deployed program would.
- Compare errors with `errors.Is(err, cache.ErrNotFound)`, never by message.

## Proof and skip reporting

- Each check prints the value or error the caller received.
- Keep `transcript.txt` and the `<backend>.yaml` files in the run's evidence directory.
- If a backend cannot be reached, report the failing `doctor` line. Do not count the backend as verified through another one.

## Features

- [Get, Set and Delete](./get-set-delete.md) covers the core read/write/remove paths on all four backends.
- [Expiry (TTL)](./ttl.md) covers entries that expire and entries that do not.
- [Choosing a backend](./registry.md) covers config files, DSNs, the environment and error messages.
- [Backend differences](./backend-differences.md) covers the documented places where memcached and the others differ.
