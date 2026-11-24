//go:build integration

package memcached

import (
	"testing"
	"time"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
	"github.com/champ-isaac/cacheconnector/memcached"
)

func benchCache(b *testing.B, opts ...memcached.Option) cache.Cache {
	c := memcached.New([]string{MemcachedHost}, append([]memcached.Option{memcached.WithTimeout(2 * time.Second)}, opts...)...)
	b.Cleanup(func() { _ = c.Close() })
	return c
}

// BenchmarkCache runs the shared benchmarks against the memcached container
// with the client's default of 2 idle connections per server. The benchmark
// keys are deleted afterwards.
func BenchmarkCache(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache { return benchCache(b) })
}

// BenchmarkCacheMaxIdle16 is the same with 16 idle connections, as many as the
// parallel benchmarks have callers. Only the parallel results should differ:
// with fewer idle connections than callers the client opens and closes a
// connection per call.
func BenchmarkCacheMaxIdle16(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache { return benchCache(b, memcached.WithMaxIdleConns(16)) })
}
