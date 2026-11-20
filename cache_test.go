package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tw.com.championtek.cache/inmemory"
	"tw.com.championtek.cache/redis"
)

var ctx = context.Background()

// backends returns one ready Cache per backend, so every test runs against both.
func backends(t *testing.T) map[string]Cache {
	t.Helper()
	mr := miniredis.RunT(t)
	r, err := New(ctx, Config{Backend: BackendRedis, Redis: []redis.Option{redis.WithAddr(mr.Addr())}})
	require.NoError(t, err)
	m, err := New(ctx, Config{Backend: BackendInMemory, InMemory: []inmemory.Option{inmemory.WithCleanupInterval(time.Hour)}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close(); _ = m.Close() })
	return map[string]Cache{"redis": r, "inmemory": m}
}

// The same behaviour is expected from both backends.
func TestCache_Contract(t *testing.T) {
	for name, c := range backends(t) {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, c.Ping(ctx))

			_, err := c.Get(ctx, "k")
			assert.ErrorIs(t, err, ErrNotFound)

			require.NoError(t, c.Set(ctx, "k", "v", 0))
			v, err := c.Get(ctx, "k")
			assert.NoError(t, err)
			assert.Equal(t, "v", v)

			require.NoError(t, c.Set(ctx, "k", "v2", time.Hour))
			v, _ = c.Get(ctx, "k")
			assert.Equal(t, "v2", v)

			require.NoError(t, c.Delete(ctx, "k"))
			_, err = c.Get(ctx, "k")
			assert.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func TestCache_TTLExpires(t *testing.T) {
	mr := miniredis.RunT(t)
	r, err := New(ctx, Config{Backend: BackendRedis, Redis: []redis.Option{redis.WithAddr(mr.Addr())}})
	require.NoError(t, err)
	require.NoError(t, r.Set(ctx, "k", "v", time.Minute))
	mr.FastForward(2 * time.Minute)
	_, err = r.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNotFound)

	m, err := New(ctx, Config{InMemory: []inmemory.Option{inmemory.WithCleanupInterval(time.Hour)}})
	require.NoError(t, err)
	defer m.Close()
	require.NoError(t, m.Set(ctx, "k", "v", 20*time.Millisecond))
	time.Sleep(40 * time.Millisecond)
	_, err = m.Get(ctx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRedisCache_ServerDown(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := New(ctx, Config{Backend: BackendRedis, Redis: []redis.Option{redis.WithAddr(mr.Addr())}})
	require.NoError(t, err)
	mr.Close()

	_, err = c.Get(ctx, "k")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound, "a broken connection is not a cache miss")
	assert.Error(t, c.Set(ctx, "k", "v", 0))
	assert.Error(t, c.Delete(ctx, "k"))
	assert.Error(t, c.Ping(ctx))
}

func TestNew(t *testing.T) {
	t.Run("empty backend is in-memory", func(t *testing.T) {
		c, err := New(ctx, Config{})
		require.NoError(t, err)
		assert.IsType(t, memoryCache{}, c)
		assert.NoError(t, c.Close())
	})
	t.Run("in-memory ignores the redis settings", func(t *testing.T) {
		t.Setenv(EnvBackend, "inmemory")
		t.Setenv("REDIS_DB", "three")
		c, err := FromEnv(ctx)
		require.NoError(t, err)
		_ = c.Close()
	})
	t.Run("unknown backend", func(t *testing.T) {
		_, err := New(ctx, Config{Backend: "memcached"})
		assert.ErrorContains(t, err, `unknown backend "memcached"`)
	})
	t.Run("redis unreachable", func(t *testing.T) {
		_, err := New(ctx, Config{Backend: BackendRedis, Redis: []redis.Option{redis.WithAddr("127.0.0.1:1")}})
		assert.Error(t, err)
	})
}

func TestFromEnv(t *testing.T) {
	t.Run("default is in-memory", func(t *testing.T) {
		t.Setenv(EnvBackend, "")
		c, err := FromEnv(ctx)
		require.NoError(t, err)
		assert.IsType(t, memoryCache{}, c)
		_ = c.Close()
	})
	t.Run("flag switches to redis and reads its settings", func(t *testing.T) {
		mr := miniredis.RunT(t)
		mr.RequireUserAuth("u", "p")
		t.Setenv(EnvBackend, "redis")
		t.Setenv("REDIS_ADDR", mr.Addr())
		t.Setenv("REDIS_USERNAME", "u")
		t.Setenv("REDIS_PASSWORD", "p")
		t.Setenv("REDIS_DB", "3")
		c, err := FromEnv(ctx)
		require.NoError(t, err)
		defer c.Close()
		assert.IsType(t, redisCache{}, c)
		require.NoError(t, c.Set(ctx, "k", "v", 0))
		mr.Select(3)
		got, err := mr.Get("k")
		assert.NoError(t, err)
		assert.Equal(t, "v", got, "REDIS_DB is honoured")
	})
	t.Run("bad redis db", func(t *testing.T) {
		t.Setenv(EnvBackend, "redis")
		t.Setenv("REDIS_DB", "three")
		_, err := FromEnv(ctx)
		assert.ErrorContains(t, err, "bad REDIS_DB")
	})
	t.Run("in-memory ignores the redis settings", func(t *testing.T) {
		t.Setenv(EnvBackend, "inmemory")
		t.Setenv("REDIS_DB", "three")
		c, err := FromEnv(ctx)
		require.NoError(t, err)
		_ = c.Close()
	})
	t.Run("unknown backend", func(t *testing.T) {
		t.Setenv(EnvBackend, "etcd")
		_, err := FromEnv(ctx)
		assert.Error(t, err)
	})
}
