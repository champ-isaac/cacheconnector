//go:build integration

package redis

import (
	"testing"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
	"github.com/champ-isaac/cacheconnector/redis"
)

// BenchmarkCache runs the shared benchmarks against the redis container, with
// the client's default pool (10 connections, 3 kept idle). The keys are named
// bench:... and deleted afterwards, so the data in database 0 is left alone.
func BenchmarkCache(b *testing.B) {
	cachetest.Bench(b, func(b *testing.B) cache.Cache {
		c, err := redis.New(ctx,
			redis.WithAddr(RedisHost),
			redis.WithUsername("default"),
			redis.WithPassword("user"),
		)
		if err != nil {
			b.Fatal(err)
		}
		cc := redis.NewCache(c)
		b.Cleanup(func() { _ = cc.Close() })
		return cc
	})
}
