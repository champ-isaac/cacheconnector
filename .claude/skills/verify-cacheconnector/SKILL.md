---
name: verify-cacheconnector
description: Prove the cacheconnector Go library works the way a consumer uses it. Opens inmemory, redis, valkey and memcached through the public API (cache.FromFile, Open, FromEnv, Get/Set/Delete/Ping) against the docker-compose servers and captures a transcript. Use after changing cache.go, registry.go or any backend package, or to check a change end to end beyond unit tests.
---

# Verify cacheconnector

Surface: a Go library, no app. "Driving" it means a small consumer program (`driver/main.go`) that imports only the root package and `all`, builds each backend from a YAML config file, and prints what a caller sees. Primary target: `cache.go` (the `Cache` interface) and `registry.go` (Open, FromFile, FromConfig, FromEnv, Backends, Config.Resolved). Backends: `inmemory` (in process, no server), `redis`, `valkey`, `memcached` (containers from `docker-compose.yaml`).

Fast path, from the repo root:

```
.claude/skills/verify-cacheconnector/scripts/verify.sh all
```

That runs up, doctor, drive, down, and exits non-zero if any check fails. Or step by step with `up`, `doctor`, `drive`, `down`. Set `RUN_ID=<name>` to name the run; the default is a timestamp, and later steps reuse the last run id.

## Launch

- `verify.sh up` runs `docker compose up -d redis valkey memcached`. It skips any that is already running and records the ones it started in `<evidence>/started`. It does not start `redis-ui`.
- The three containers already exist on this machine as stopped containers of this compose project. `up` restarts them, with their old volumes. That is why the driver uses only its own key prefix and never flushes (see Gotchas).
- Ready means usable: `up` polls the doctor (open, Ping, Set/Get round trip on every backend) for up to 30 seconds.
- Host ports: redis `127.0.0.1:6381`, valkey `127.0.0.1:6380`, memcached `127.0.0.1:11211`. Password `user` for redis and valkey, none for memcached. Docker must be running.

## Doctor

`verify.sh doctor` opens each backend from its YAML file, checks `Config.Resolved()` names the right driver, runs `Ping`, and does one Set/Get/Delete round trip. Run it first when anything looks off. If a port is owned by something else (for example another redis on 6379/6380/6381), stop and say so; do not point the driver at it.

## Drive

`verify.sh drive` runs `go run ./.claude/skills/verify-cacheconnector/driver -mode drive`. Go runs a directory under `.claude/` only when it is named explicitly, so `go test ./...` and `go vet ./...` never see the driver.

It checks the `registry` section once, then each backend with the same journeys: `Ping`, Get on a missing key (`cache.ErrNotFound`), Set then Get, overwrite, a binary value, a changed returned slice not changing the cache, Delete then Get, Delete of a missing key, an unusual-but-legal key, the empty key, a cancelled context, and a 2 second TTL expiring. Each line is `[PASS|FAIL] what -> what the caller saw`. Per-feature recipes and expected outcomes are in [features/](features/README.md). Read the matching file before changing the driver.

## Evidence

Everything goes to `.claude/verify-evidence/<run-id>/` (git-ignored):

- `transcript.txt`: every check with the value or error the caller got.
- `doctor.txt`: the last doctor run.
- `<backend>.yaml`: the exact config file each backend was opened from.
- `started`: containers this run started.

Proof standards:

- Use only the public API. Never import `redis/`, `valkey/` or `memcached/` in the driver, and never read server state through a back door to make a check pass.
- A PASS must show the observed value or error, not just "ok".
- A backend difference that the docs describe (empty key on memcached) is asserted as the documented behaviour. A difference the docs do not describe is a finding: report it, do not hide it.
- For a second view of a mutation, ask the server directly, for example `docker exec redis redis-cli -a user -n 15 --scan --pattern 'verify-*'` (use `valkey-cli` in the `valkey` container). After a run it should print nothing.

## Cleanup

`verify.sh down` runs `docker compose stop` for only the services listed in `started`. Containers that were already running stay running. Nothing is removed, and no volume is touched. The evidence directory is never deleted by cleanup. After a failed run, run `down` anyway.

## Gotchas

- Never flush. The existing integration tests flush memcached and redis/valkey database 1; this skill does neither. Redis and valkey use database 15, memcached has no databases, so every key carries the prefix `verify-<run-id>:` and the driver deletes its own keys at the end. The empty key is deleted by name.
- Memcached rounds a TTL up to whole seconds and can expire an entry up to one second early, so the TTL check uses 2 seconds, then reads at 3.2 seconds.
- Memcached opens its connection lazily. A down memcached shows up at the first Get/Set, and `Ping` is what reports it.
- A `go.mod` change (new Go or dependency) can make the first `go run` slow. Wait for it.
- One run takes about 10 seconds, most of it the four TTL waits.
