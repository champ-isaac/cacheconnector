# Backend differences

The `Cache` interface behaves the same on every backend except where the docs say otherwise. This file lists the differences a caller can see and that the driver asserts.

## Sub-features

- `diff-empty-key` On `inmemory`, `redis` and `valkey`, Set, Get and Delete accept the empty key. On `memcached` Set and Get return an error that is not `ErrNotFound`.
- `diff-cancelled-ctx` Get with an already cancelled context returns promptly on every backend.

## How to get to it (user POV)

- `c.Set(ctx, "", value, ttl)` and `c.Get(ctx, "")`.
- A `ctx` cancelled before the call.

## Driving it with verify.sh

Preconditions: baseline from the README.

- **Empty key.** `verify.sh drive`. The `inmemory`, `redis` and `valkey` sections show `Set("") and Get("") work`. The `memcached` section shows `Set("") and Get("") return an error that is not ErrNotFound` with the `CLIENT_ERROR` text.
- **Cancelled context.** Every section shows `Get with a cancelled context returns promptly -> returned`. The driver does not assert whether the call errors, only that it does not hang.

## Gotchas

- Not covered: the 250-byte key limit and keys with spaces on memcached (`memcache.ErrMalformedKey`), and deadline errors on a silent server (`context.DeadlineExceeded` on redis and valkey). Add a journey if those paths change.
