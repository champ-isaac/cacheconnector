package inmemory

import (
	"testing"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

// BenchmarkCache is the in-process baseline: no network, so it shows the
// library's own cost, which the network backends add to.
func BenchmarkCache(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache {
		c := NewCache(New())
		b.Cleanup(func() { _ = c.Close() })
		return c
	})
}
