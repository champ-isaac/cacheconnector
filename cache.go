// Package cache defines one Cache interface over several key/value backends:
// Redis, Valkey, Memcached and an in-process memory cache.
//
// The root package has no backend dependency. Each backend lives in its own
// package and registers itself in init, the way database/sql drivers do.
// Import the "all" package to compile every backend into one binary, then
// choose the backend by name at start-up from a config file:
//
//	import (
//		"github.com/champ-isaac/cacheconnector"
//		_ "github.com/champ-isaac/cacheconnector/all"
//	)
//
//	c, err := cache.FromFile(ctx, "cache.yaml") // driver: redis
//
// Changing the driver in the file and restarting selects the new backend;
// nothing is recompiled. A program that wants a smaller binary imports only
// the backend packages it needs, for example _ ".../redis".
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Get when the key is missing or expired.
var ErrNotFound = errors.New("cache: key not found")

// Cache is what every backend supports. Values are opaque bytes; a backend
// never returns a slice it still holds, so callers may modify what they get.
//
// A ttl of zero or less means the entry does not expire. Backends with a
// coarser TTL clock (Memcached counts whole seconds) round a ttl up, but an
// entry can still live up to one second shorter than asked. Memcached also stores
// the expiry as a 32-bit Unix time, so an expiry after 19 January 2038 (Unix time
// 2147483647, 03:14:07 UTC) is clamped to that moment. Delete of a missing key is
// not an error. A Cache is safe for concurrent use.
//
// The backends differ in two more ways. Valkey and Redis honour a call's context
// deadline: a call stuck on a slow or silent server returns an error that wraps
// context.DeadlineExceeded when the deadline passes. Memcached does not: it checks
// ctx only before a call starts, so a deadline that passes during the call does not
// cut it short, and only the client's own timeout bounds the call. Memcached also
// rejects the empty key: the server refuses it, and Get, Set and Delete return an
// error (not ErrNotFound, and not memcache.ErrMalformedKey, which the client
// returns for other bad keys). Valkey, Redis and the in-memory backend accept the
// empty key like any other.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Ping(ctx context.Context) error
	Close() error
}
