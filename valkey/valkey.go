// Package valkey registers the "valkey" and "valkeys" (TLS) cache backends.
//
// Valkey is wire-compatible with Redis, so this package reuses the Redis
// client and only changes the names: DSNs start with valkey:// and, when a
// DSN has no host, settings come from VALKEY_ADDR, VALKEY_USERNAME,
// VALKEY_PASSWORD and VALKEY_DB. See redis.Factory for the DSN format.
package valkey

import (
	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/redis"
)

func init() {
	cache.Register("valkey", redis.Factory("VALKEY", false), cache.WithTLS("valkeys"))
	cache.Register("valkeys", redis.Factory("VALKEY", true))
}
