//go:build integration

package redis

import (
	"testing"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
	"github.com/champ-isaac/cacheconnector/redis"
)

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
