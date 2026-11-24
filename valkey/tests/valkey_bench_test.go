//go:build integration

package valkey

import (
	"testing"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

// BenchmarkCache runs the shared benchmarks against the valkey container
// through the valkey:// DSN, so the client's default pool is used. Valkey
// reuses the Redis client, so a difference from the redis numbers comes from
// the server. The benchmark keys are deleted afterwards.
func BenchmarkCache(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache {
		c, err := cache.Open(ctx, dsn())
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { _ = c.Close() })
		return c
	})
}
