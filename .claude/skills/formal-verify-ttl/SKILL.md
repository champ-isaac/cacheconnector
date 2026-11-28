---
name: formal-verify-ttl
description: Exhaustively checks the TTL promise of cacheconnector, that a value stored with a positive ttl lives exactly that long and a value stored with ttl 0 or less never expires. Also checks that the inmemory cleanup interval can never be zero or negative, and that the background janitor sweeps and stops. Covers memcached.expiration and the inmemory Client (Set, Get, Delete, sweep, WithCleanupInterval, janitor, Close). Use after any change to memcached/client.go, inmemory/client.go, or how a ttl is passed to a backend, and before trusting a green unit test on expiry.
sync-commit: bbd1d7c4ffd5a5792b27bf42c47224be26f3b08b
---

# Formal check of the cacheconnector TTL promise

## The promise and the harm

**Promise.** `Set(ctx, key, value, ttl)` with `ttl > 0` makes the value disappear when the ttl ends, rounded up to a whole second on memcached. With `ttl <= 0` the value never expires.

**Harm if broken.** A value that should expire lives forever, so users get stale sessions, tokens or prices. Or a value expires at once or early, so the cache never hits and the database takes the load. Neither shows up as an error.

`/verify-cacheconnector` tests one 2 second ttl end to end. It never tries a ttl over 30 days, a sub-second ttl, a ttl past the year 2038, or a long run of Set, wait and sweep steps. This skill covers those.

## Tool and why

Plain `go test`, run on the real code through `go test -overlay`. `check` and `mutants` copy nothing into the repo and edit no product file. `fuzz` is the one exception: `go test -fuzz` needs the check file inside the package, so it copies `expiration_test.go` to `memcached/zz_formal_test.go` for the run and removes it on exit. A hard kill can leave that file behind, so delete it and never commit it.

- No formal verifier for Go is practical to install here. Gobra needs a separate Viper toolchain and annotations in the product code. A TLA+ model would be a hand-made copy of the code, and this promise is not about event order.
- So the check enumerates every case in a stated bound and compares the real code with a short independent spec. Inside the bound it covers every case. Outside it, it proves nothing.

## Run it

From the repo root:

```
.claude/skills/formal-verify-ttl/scripts/verify.sh check      # about 10 s, run on every change to a covered file
.claude/skills/formal-verify-ttl/scripts/verify.sh mutants    # about 70 s, proves the check can fail
.claude/skills/formal-verify-ttl/scripts/verify.sh fuzz 60s   # optional random search beyond the bound
.claude/skills/formal-verify-ttl/scripts/verify.sh drift      # which covered files changed since the sync commit
```

`check` prints `ok` for `memcached` and `inmemory` when everything holds. Any other output is a failure. Read it as below.

## What is checked

There are four checks. 1 and 2 cover the TTL promise. 3 and 4 cover the background cleanup that keeps memory bounded.

**1. `memcached.expiration(ttl, now) int32`, in `memcached/client.go`.** The spec models what a memcached server does with the number: 0 is never, 1 to 2592000 (30 days) is seconds from now, anything bigger is a unix timestamp, and the value must fit an int32. For each case the check works out the moment memcached would drop the entry and compares it with `now + ceil(ttl)`.

- Rules checked: a ttl of 0 or less gives 0. A positive ttl never gives 0. The drop time equals `now + ceil(ttl)` exactly. A ttl so long that the timestamp would pass `MaxInt32` (19 Jan 2038) is cut to `MaxInt32`.
- Bound: every whole second from -5 to 3,000,000 (35 days), each with the extra amounts 0, 1 ns, 500 ms and 999,999,999 ns. That is repeated for seven clock values from 1970 to just before 2038. Plus +/-100 seconds around each switch point (30 days, the `MaxInt32` ceiling, 2^32, the largest ttl a Duration can hold), plus a coarse sweep up to the largest ttl, plus `MinInt64` and `MaxInt64`. About 84 million cases.
- Assumption: the clock is before 19 Jan 2038. See Known limits.

**2. `inmemory.Client`, in `inmemory/client.go`.** Every sequence of up to 6 operations over 13 operations: `Set` with ttl 0, 1s, 2s or -1s on one of two keys, `Delete` on one of two keys, advance the clock 1s or 2s, and run `sweep`. That is 5.2 million sequences. After every step, `Get` on both keys must match a plain reference: live until the ttl ends, never expired, never lost. After a `sweep`, no expired entry may stay in memory.

**3. The cleanup interval, `inmemory.WithCleanupInterval` and `New`, in `inmemory/client.go`.** The janitor calls `time.NewTicker(c.interval)`, which panics on an interval of zero or less, and the panic kills the whole process. The check applies every sequence of up to 3 `WithCleanupInterval` options over 13 durations (`MinInt64`, -1h, -1s, -1ns, 0, 1ns, 1s, 1h, `MaxInt64` and others). After each one the interval must equal the last positive value given, or the one minute default, and must never be zero or negative. It also starts `New()` and a real client for each of 11 durations, waits 50 ms so a crashing janitor would show, then checks `Set`, `Get` and `Close` still work. This is the guard added in #19 (`cd83051`).

**4. The janitor loop and `Close`, in `inmemory/client.go`.** A client with a 2 ms interval and a fake clock holds one entry that expires, one that has not, and one that never expires. Within 5 seconds the janitor must remove the expired one and only that one. Then 8 callers run `Close` at once. All must return within 5 seconds, and the janitor goroutine must have stopped. Unlike checks 1 to 3, this one depends on real time, so it uses wide limits. If it fails on a loaded machine, rerun it once before suspecting a bug.

## Reading a result

- **A counterexample from check 1** names the ttl, the clock, and when memcached would drop the entry against when it should. Check it by hand, then fix `expiration` or, if the promise changed on purpose, change `problem` in `checks/memcached/expiration_test.go`.
- **A failure from check 3 or 4** names the interval or the step. A `panic: non-positive interval for NewTicker` means the janitor was started with a bad interval, which is the #19 regression.
- **A counterexample from check 2** is the shortest operation list the search finds at that length, for example `set(ttl=1s)(k0), tick1s: Get(k0) = "v0", true but the entry should be gone`. Replay it in a unit test.
- **A bug in the check itself** is a real possibility. Before changing product code, ask whether the spec in `problem` or `compare` says what the doc says. Do not loosen the spec just to get a pass.
- `STALE`, `BROKEN` or `MISSED` from `mutants` mean the check no longer proves what it did. `STALE` means a covered line was reworded, so update the pattern in `scripts/verify.sh`. `MISSED` means a deliberate break got through, so the check has a hole.

## Coverage record

| Check | Code it describes |
| --- | --- |
| `checks/memcached/expiration_test.go` | `memcached/client.go`: `expiration`, `maxRelativeTTL`, and the ttl argument of `Client.Set` |
| `checks/inmemory/lifetime_test.go` | `inmemory/client.go`: `Client.Set`, `Get`, `Delete`, `sweep`, `entry.expired` |
| `checks/inmemory/cleanup_test.go` | `inmemory/client.go`: `WithCleanupInterval`, `New` (the default interval), `janitor`, `Close` |

`inmemory/cache.go` passes the ttl straight to `Client.Set` and is covered by reading, not by a check. Its `factory` rejects a `cleanup_interval` of zero or less before it reaches `WithCleanupInterval`, and only the repo's unit tests cover that path. `redis/cache.go` clamps a negative ttl to 0 and hands the rest to go-redis, so it is not covered.

**sync-commit** (the line in the frontmatter) is the commit at which the check was confirmed to describe the covered files. `verify.sh drift` lists covered files that changed after it. When it lists any, re-read the changed code against the table above, update the check or the mutants if needed, run `check` and `mutants`, and only then move `sync-commit` forward to the current commit. Never move it first.

## Known limits

- **After 19 Jan 2038** the memcached ttl is a 32-bit number, so `expiration` returns `MaxInt32` for every ttl over 30 days, a time already in the past. Those entries would expire at once. The check assumes a clock before that date and does not test it. Nothing today is affected.
- **Not covered:** the redis and valkey server side, memcached's own handling of the number, clock differences between this machine and the memcached server (an absolute timestamp for a ttl over 30 days shifts by the difference), concurrent callers (only one at a time is checked), so **locking is not checked**: a `sweep` that takes only a read lock, or any other race between the janitor and `Get` or `Set`, passes every check here (use `go test -race ./inmemory` for that), and exact janitor timing (check 4 uses wide limits and does not bound how often the janitor runs), and the `cleanup_interval` DSN parameter.
- A pass is a pass inside the bounds above, not a proof for every input.
