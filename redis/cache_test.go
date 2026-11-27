package redis

import (
	"context"
	"net/url"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

func TestCacheContract(t *testing.T) {
	mr := miniredis.RunT(t)
	cachetest.Run(t, func(t *testing.T) cache.Cache {
		mr.FlushAll()
		c, err := New(context.Background(), WithAddr(mr.Addr()))
		require.NoError(t, err)
		return NewCache(c)
	}, mr.FastForward)
}

func TestFactoryDSN(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	mr.RequireUserAuth("alice", "s3cret")

	c, err := cache.Open(ctx, "redis://alice:s3cret@"+mr.Addr()+"/2")
	require.NoError(t, err)
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))
	require.NoError(t, c.Close())
	mr.Select(2)
	got, err := mr.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "v", got, "the DSN path selected database 2")

	_, err = cache.Open(ctx, "redis://alice:wrong@"+mr.Addr())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "wrong")
}

func TestFactoryTLS(t *testing.T) {
	ctx := context.Background()
	addr, caPath := tlsServer(t)

	c, err := cache.Open(ctx, "rediss://"+addr+"?ca_cert="+url.QueryEscape(caPath)+"&servername=redis.test")
	require.NoError(t, err)
	assert.NoError(t, c.Ping(ctx))
	require.NoError(t, c.Close())

	_, err = cache.Open(ctx, "rediss://"+addr+"?servername=redis.test")
	assert.Error(t, err, "TLS needs ca_cert")
	_, err = cache.Open(ctx, "rediss:")
	assert.Error(t, err, "TLS needs a host")
}

func TestFactoryFromEnv(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	t.Setenv("REDIS_ADDR", mr.Addr())
	t.Setenv(cache.EnvBackend, "redis")
	t.Setenv(cache.EnvURL, "")

	c, err := cache.FromEnv(ctx)
	require.NoError(t, err)
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))
	require.NoError(t, c.Close())

	t.Setenv("REDIS_DB", "x")
	_, err = cache.FromEnv(ctx)
	assert.ErrorContains(t, err, "REDIS_DB")
}

func TestOptionsFromURLBadDatabase(t *testing.T) {
	for _, db := range []string{"/abc", "/-1"} {
		_, err := OptionsFromURL(&url.URL{Scheme: "redis", Host: "h:1", Path: db}, "REDIS", false)
		assert.Error(t, err, db)
	}
}

func TestAdapterUnreachableServer(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := New(context.Background(), WithAddr(mr.Addr()))
	require.NoError(t, err)
	a := NewCache(c)
	mr.Close()
	_, err = a.Get(context.Background(), "k")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, cache.ErrNotFound)
	assert.NoError(t, a.Close())
}

func TestOptionsFromURLServerNameDefaultsToHost(t *testing.T) {
	opts, err := OptionsFromURL(&url.URL{Scheme: "rediss", Host: "cache.example.com:6380", RawQuery: "ca_cert=/ca.pem"}, "REDIS", true)
	require.NoError(t, err)
	c := &Client{}
	for _, o := range opts {
		o(c)
	}
	assert.Equal(t, "cache.example.com", c.serverName)
	assert.Equal(t, "/ca.pem", c.caCertPath)
	assert.True(t, c.enableTLS)
}
