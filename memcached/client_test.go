package memcached

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

var ctx = context.Background()

func TestCache_Contract(t *testing.T) {
	srv := newFakeServer(t)
	cachetest.Run(t, func(t *testing.T) cache.Cache {
		srv.mu.Lock()
		srv.data = map[string]fakeItem{}
		srv.mu.Unlock()
		return New([]string{srv.Addr()}, WithTimeout(2*time.Second))
	}, srv.advance)
}

func TestExpiration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name string
		ttl  time.Duration
		want int32
	}{
		{"zero means none", 0, 0},
		{"negative means none", -time.Hour, 0},
		{"sub-second rounds up", 500 * time.Millisecond, 1},
		{"whole second", time.Second, 1},
		{"fraction rounds up", 1500 * time.Millisecond, 2},
		{"30 days is still relative", 30 * 24 * time.Hour, 30 * 24 * 3600},
		{"beyond 30 days is a unix timestamp", 30*24*time.Hour + time.Second, int32(now.Unix()) + 30*24*3600 + 1},
		{"clamped to int32", 100 * 365 * 24 * time.Hour, math.MaxInt32},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, expiration(tc.ttl, now))
		})
	}
}

func TestSet_TTLOnTheWire(t *testing.T) {
	srv := newFakeServer(t)
	c := New([]string{srv.Addr()}, WithTimeout(2*time.Second))
	defer func() { assert.NoError(t, c.Close()) }()

	require.NoError(t, c.Set(ctx, "k", []byte("v"), 1500*time.Millisecond))
	assert.EqualValues(t, 2, srv.lastExptime)

	// Past 30 days Memcached wants an absolute time, taken from the client clock.
	c.now = srv.now
	require.NoError(t, c.Set(ctx, "far", []byte("v"), 31*24*time.Hour))
	assert.Equal(t, srv.now().Add(31*24*time.Hour).Unix(), srv.lastExptime)
	_, err := c.Get(ctx, "far")
	assert.NoError(t, err)
}

func TestNew_DefaultAddr(t *testing.T) {
	c := New(nil)
	assert.NoError(t, c.Close())
}

func TestFactory_DSN(t *testing.T) {
	a, b := newFakeServer(t), newFakeServer(t)
	c, err := cache.Open(ctx, "memcached://"+a.Addr()+"?server="+b.Addr()+"&timeout=2s&max_idle_conns=4")
	require.NoError(t, err)
	assert.NoError(t, c.Ping(ctx), "both servers answer")
	require.NoError(t, c.Close())

	for _, bad := range []string{
		"memcached://h:1?timeout=soon",
		"memcached://h:1?timeout=-1s",
		"memcached://h:1?max_idle_conns=x",
		"memcached://h:1?max_idle_conns=0",
	} {
		_, err = cache.Open(ctx, bad)
		assert.Error(t, err, bad)
	}
}

func TestFactory_FromEnv(t *testing.T) {
	srv := newFakeServer(t)
	t.Setenv("MEMCACHED_ADDRS", " "+srv.Addr()+" , ,")
	t.Setenv(cache.EnvBackend, "memcached")
	t.Setenv(cache.EnvURL, "")
	c, err := cache.FromEnv(ctx)
	require.NoError(t, err)
	assert.NoError(t, c.Ping(ctx))
	require.NoError(t, c.Close())

	t.Setenv("MEMCACHED_ADDRS", "")
	c, err = cache.FromEnv(ctx) // falls back to 127.0.0.1:11211, not contacted here
	require.NoError(t, err)
	require.NoError(t, c.Close())
}

func TestErrors(t *testing.T) {
	down := New([]string{"127.0.0.1:1"}, WithTimeout(200*time.Millisecond))
	defer func() { _ = down.Close() }()
	_, err := down.Get(ctx, "k")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, cache.ErrNotFound, "an unreachable server is not a miss")
	assert.Error(t, down.Set(ctx, "k", []byte("v"), 0))
	assert.Error(t, down.Delete(ctx, "k"))
	assert.Error(t, down.Ping(ctx))

	srv := newFakeServer(t)
	c := New([]string{srv.Addr()}, WithTimeout(2*time.Second))
	defer func() { assert.NoError(t, c.Close()) }()
	assert.ErrorIs(t, c.Set(ctx, "bad key", []byte("v"), 0), memcache.ErrMalformedKey)
	assert.ErrorIs(t, c.Delete(ctx, "bad key"), memcache.ErrMalformedKey, "only a miss is swallowed")

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	assert.ErrorIs(t, c.Set(canceled, "k", []byte("v"), 0), context.Canceled)
	assert.ErrorIs(t, c.Delete(canceled, "k"), context.Canceled)
	assert.ErrorIs(t, c.Ping(canceled), context.Canceled)
}
