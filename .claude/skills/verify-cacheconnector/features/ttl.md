# Expiry (TTL)

A caller can give an entry a lifetime. After it passes, Get returns `cache.ErrNotFound`. A ttl of zero or less means the entry never expires.

## Sub-features

- `ttl-live` An entry with a 2 second ttl is readable right after Set.
- `ttl-expire` The same entry returns `ErrNotFound` after 3.2 seconds.
- `ttl-none` A value set with ttl 0 is still readable (covered by the binary-value check).

## How to get to it (user POV)

- `c.Set(ctx, key, value, 2*time.Second)` then `c.Get`.

## Driving it with verify.sh

Preconditions: baseline from the README.

- **Expiry.** `verify.sh drive`. Each backend section ends with `Get right after Set with ttl 2s` showing `got "soon"` and `Get after the ttl has passed` showing `cache: key not found`.

## Gotchas

- Memcached rounds a ttl up to whole seconds and can expire an entry up to one second early. A 1 second ttl can already be gone at the first Get, so never shorten the 2 second value.
- Memcached stores expiry as a 32-bit Unix time, so a ttl that ends after 19 January 2038 is clamped. The driver does not test that.
- Each backend adds about 3.2 seconds of waiting.
