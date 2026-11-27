package inmemory

import (
	"testing"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

func BenchmarkCache(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache {
		c := NewCache(New())
		b.Cleanup(func() { _ = c.Close() })
		return c
	})
}
