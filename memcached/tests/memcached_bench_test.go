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

func BenchmarkCache(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache { return benchCache(b) })
}

func BenchmarkCacheMaxIdle16(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache { return benchCache(b, memcached.WithMaxIdleConns(16)) })
}
