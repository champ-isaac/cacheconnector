//go:build integration

package valkey

import (
	"testing"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

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
