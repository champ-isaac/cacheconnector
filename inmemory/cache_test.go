package inmemory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

func TestCache_Contract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) cache.Cache {
		return NewCache(New(WithCleanupInterval(time.Hour)))
	}, nil)
}

func TestFactory(t *testing.T) {
	ctx := context.Background()

	c, err := cache.Open(ctx, "inmemory:")
	require.NoError(t, err)
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))
	require.NoError(t, c.Close())

	c, err = cache.Open(ctx, "inmemory:?cleanup_interval=5s")
	require.NoError(t, err)
	require.NoError(t, c.Close())

	for _, bad := range []string{"inmemory:?cleanup_interval=soon", "inmemory:?cleanup_interval=-1s"} {
		_, err = cache.Open(ctx, bad)
		assert.Error(t, err, bad)
	}
}

func TestCache_CanceledContext(t *testing.T) {
	c := NewCache(New())
	defer func() { assert.NoError(t, c.Close()) }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, c.Set(ctx, "k", []byte("v"), 0), context.Canceled)
	assert.ErrorIs(t, c.Delete(ctx, "k"), context.Canceled)
	assert.ErrorIs(t, c.Ping(ctx), context.Canceled)
}
