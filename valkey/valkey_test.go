package valkey

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

func TestCacheContract(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	cachetest.Run(t, func(t *testing.T) cache.Cache {
		mr.FlushAll()
		c, err := cache.Open(ctx, "valkey://"+mr.Addr())
		require.NoError(t, err)
		return c
	}, mr.FastForward)
}

func TestRegistered(t *testing.T) {
	assert.Subset(t, cache.Backends(), []string{"valkey", "valkeys"})
}

func TestFromEnvUsesValkeyVariables(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	t.Setenv("VALKEY_ADDR", mr.Addr())
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")
	t.Setenv(cache.EnvBackend, "valkey")
	t.Setenv(cache.EnvURL, "")

	c, err := cache.FromEnv(ctx)
	require.NoError(t, err)
	assert.NoError(t, c.Ping(ctx))
	require.NoError(t, c.Close())
}
