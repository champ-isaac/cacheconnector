---
name: formal-verify-ttl
description: Exhaustively checks the TTL promise of cacheconnector, that a value stored with a positive ttl lives exactly that long and a value stored with ttl 0 or less never expires. Covers memcached.expiration and the inmemory Client (Set, Get, Delete, sweep). Use after any change to memcached/client.go, inmemory/client.go, or how a ttl is passed to a backend, and before trusting a green unit test on expiry.
sync-commit: a68e4549bc4cbaf9dc21965245ddd4d29be580d2
---

# Formal check of the cacheconnector TTL promise

## The promise and the harm

**Promise.** `Set(ctx, key, value, ttl)` with `ttl > 0` makes the value disappear when the ttl ends, rounded up to a whole second on memcached. With `ttl <= 0` the value never expires.

**Harm if broken.** A value that should expire lives forever, so users get stale sessions, tokens or prices. Or a value expires at once or early, so the cache never hits and the database takes the load. Neither shows up as an error.

`/verify-cacheconnector` tests one 2 second ttl end to end. It never tries a ttl over 30 days, a sub-second ttl, a ttl past the year 2038, or a long run of Set, wait and sweep steps. This skill covers those.

## Tool and why

Plain `go test`, run on the real code through `go test -overlay`. Nothing is copied into the repo and no product file is edited.

- No formal verifier for Go is practical to install here. Gobra needs a separate Viper toolchain and annotations in the product code. A TLA+ model would be a hand-made copy of the code, and this promise is not about event order.
- So the check enumerates every case in a stated bound and compares the real code with a short independent spec. Inside the bound it covers every case. Outside it, it proves nothing.

## Run it

From the repo root:

```
.claude/skills/formal-verify-ttl/scripts/verify.sh check      # about 10 s, run on every change to a covered file
.claude/skills/formal-verify-ttl/scripts/verify.sh mutants    # about 40 s, proves the check can fail
.claude/skills/formal-verify-ttl/scripts/verify.sh fuzz 60s   # optional random search beyond the bound
.claude/skills/formal-verify-ttl/scripts/verify.sh drift      # which covered files changed since the sync commit
```

`check` prints `ok` for `memcached` and `inmemory` when everything holds. Any other output is a failure. Read it as below.

## What is checked

**1. `memcached.expiration(ttl, now) int32`, in `memcached/client.go`.** The spec models what a memcached server does with the number: 0 is never, 1 to 2592000 (30 days) is seconds from now, anything bigger is a unix timestamp, and the value must fit an int32. For each case the check works out the moment memcached would drop the entry and compares it with `now + ceil(ttl)`.

- Rules checked: a ttl of 0 or less gives 0. A positive ttl never gives 0. The drop time equals `now + ceil(ttl)` exactly. A ttl so long that the timestamp would pass `MaxInt32` (19 Jan 2038) is cut to `MaxInt32`.
- Bound: every whole second from -5 to 3,000,000 (35 days), each with the extra amounts 0, 1 ns, 500 ms and 999,999,999 ns. That is repeated for seven clock values from 1970 to just before 2038. Plus +/-100 seconds around each switch point (30 days, the `MaxInt32` ceiling, 2^32, the largest ttl a Duration can hold), plus a coarse sweep up to the largest ttl, plus `MinInt64` and `MaxInt64`. About 84 million cases.
- Assumption: the clock is before 19 Jan 2038. See Known limits.

**2. `inmemory.Client`, in `inmemory/client.go`.** Every sequence of up to 6 operations over 13 operations: `Set` with ttl 0, 1s, 2s or -1s on one of two keys, `Delete` on one of two keys, advance the clock 1s or 2s, and run `sweep`. That is 5.2 million sequences. After every step, `Get` on both keys must match a plain reference: live until the ttl ends, never expired, never lost. After a `sweep`, no expired entry may stay in memory.

## Reading a result

- **A counterexample from check 1** names the ttl, the clock, and when memcached would drop the entry against when it should. Check it by hand, then fix `expiration` or, if the promise changed on purpose, change `problem` in `checks/memcached/expiration_test.go`.
- **A counterexample from check 2** is the shortest operation list the search finds at that length, for example `set(ttl=1s)(k0), tick1s: Get(k0) = "v0", true but the entry should be gone`. Replay it in a unit test.
- **A bug in the check itself** is a real possibility. Before changing product code, ask whether the spec in `problem` or `compare` says what the doc says. Do not loosen the spec just to get a pass.
- `STALE`, `BROKEN` or `MISSED` from `mutants` mean the check no longer proves what it did. `STALE` means a covered line was reworded, so update the pattern in `scripts/verify.sh`. `MISSED` means a deliberate break got through, so the check has a hole.

## Coverage record

| Check | Code it describes |
| --- | --- |
| `checks/memcached/expiration_test.go` | `memcached/client.go`: `expiration`, `maxRelativeTTL`, and the ttl argument of `Client.Set` |
| `checks/inmemory/lifetime_test.go` | `inmemory/client.go`: `Client.Set`, `Get`, `Delete`, `sweep`, `entry.expired` |

`inmemory/cache.go` passes the ttl straight to `Client.Set` and is covered by reading, not by a check. `inmemory.WithCleanupInterval` and the janitor loop are in a covered file but are not part of this promise. They only set how often `sweep` runs, and the check calls `sweep` by hand. A non-positive interval is ignored since #19 (`cd83051`), and `TestNonPositiveCleanupIntervalKeepsDefault` in `inmemory/client_test.go` guards that. `redis/cache.go` clamps a negative ttl to 0 and hands the rest to go-redis, so it is not covered.

**sync-commit** (the line in the frontmatter) is the commit at which the check was confirmed to describe the covered files. `verify.sh drift` lists covered files that changed after it. When it lists any, re-read the changed code against the table above, update the check or the mutants if needed, run `check` and `mutants`, and only then move `sync-commit` forward to the current commit. Never move it first.

## Known limits

- **After 19 Jan 2038** the memcached ttl is a 32-bit number, so `expiration` returns `MaxInt32` for every ttl over 30 days, a time already in the past. Those entries would expire at once. The check assumes a clock before that date and does not test it. Nothing today is affected.
- **Not covered:** the redis and valkey server side, memcached's own handling of the number, clock differences between this machine and the memcached server (an absolute timestamp for a ttl over 30 days shifts by the difference), concurrent callers (only one at a time is checked), and `Close`, janitor timing, and the cleanup interval option.
- A pass is a pass inside the bounds above, not a proof for every input.
