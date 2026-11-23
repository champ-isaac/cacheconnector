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
// coarser TTL clock (Memcached counts whole seconds) round a ttl up, so an
// entry never lives shorter than asked. Delete of a missing key is not an
// error. A Cache is safe for concurrent use.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Ping(ctx context.Context) error
	Close() error
}
