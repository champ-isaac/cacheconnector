# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- **Breaking.** Five exported identifiers are renamed in [#16](https://github.com/champ-isaac/cacheconnector/pull/16) (`2602fd9`). The old names are removed, with no deprecated aliases, so code that uses them stops compiling until it switches to the new name. Behavior is unchanged.
  - `redis.WithEnableTls` → `redis.WithEnableTLS`. Replace the option name in your `redis.New(...)` calls.
  - `(*redis.Client).FlushDb` → `(*redis.Client).FlushDB`. Replace the method name at each call site.
  - `(*redis.Client).SMember` → `(*redis.Client).SMembers`. Replace the method name at each call site. It still wraps the Redis `SMEMBERS` command.
  - `(*redis.Client).HGetAllNScan` → `(*redis.Client).HGetAllScan`. Replace the method name at each call site.
  - `cache.Config.Resolved` → `cache.Config.DriverName`. Replace the method name, including on an embedded config such as `cfg.Cache.DriverName()`.

### Fixed

- `inmemory.WithCleanupInterval(0)` no longer crashes the process. A zero or negative interval made the background janitor call `time.NewTicker` with a non-positive duration, which panics on a goroutine the caller cannot recover from. The option now ignores a non-positive value and the default of one minute stays. Fixed in [#19](https://github.com/champ-isaac/cacheconnector/pull/19). No change is needed in calling code.

[Unreleased]: https://github.com/champ-isaac/cacheconnector/commits/develop
