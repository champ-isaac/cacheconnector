# Get, Set and Delete

A caller stores bytes under a key, reads them back, replaces them and removes them. A missing key is `cache.ErrNotFound`, on every backend.

## Sub-features

- `gsd-miss` Get of a missing key returns `cache.ErrNotFound` and no value.
- `gsd-roundtrip` Set then Get returns the same bytes.
- `gsd-overwrite` A second Set replaces the first value.
- `gsd-binary` Zero bytes, non-UTF-8 bytes, CR, LF and spaces in a value come back unchanged.
- `gsd-own-slice` Changing a slice returned by Get does not change what the cache holds.
- `gsd-delete` Delete then Get returns `ErrNotFound`.
- `gsd-delete-missing` Delete of a missing key returns nil.
- `gsd-key-chars` Keys with `:`, `/`, `-` and `.` work.

## How to get to it (user POV)

- `c.Get(ctx, key)`, `c.Set(ctx, key, value, ttl)`, `c.Delete(ctx, key)` on a `cache.Cache` from `cache.FromFile`.

## Driving it with verify.sh

Preconditions: baseline from the README; `verify.sh doctor` passes.

- **Run it.** `verify.sh drive`. In the transcript, each backend section shows `Get(missing)`, `Set then Get`, `Set again overwrites`, `binary value round trips`, `modifying a returned slice...`, `Delete then Get` and `Delete of a missing key...` as `[PASS]` with the observed value, such as `got "hello"`.
- **Second view.** After the run, `docker exec redis redis-cli -a user -n 15 --scan --pattern 'verify-*'` prints nothing, and the same with `valkey`. This shows Delete and the end-of-run cleanup reached the server.

## Gotchas

- Redis and valkey use database 15. A DSN without a database would write to database 0.
- Do not assert on error text. Only `errors.Is(err, cache.ErrNotFound)` is part of the contract.
