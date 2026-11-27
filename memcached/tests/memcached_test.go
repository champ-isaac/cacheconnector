//go:build integration

package memcached

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
	"github.com/champ-isaac/cacheconnector/memcached"
)

func TestCache_Contract(t *testing.T) {
	cachetest.Run(t, func(t *testing.T) cache.Cache { return newCache(t) }, nil)
}

func TestFromEnv_UsesMemcachedAddrs(t *testing.T) {
	t.Setenv("MEMCACHED_ADDRS", MemcachedHost)
	t.Setenv(cache.EnvBackend, "memcached")
	t.Setenv(cache.EnvURL, "")

	c, err := cache.FromEnv(ctx)
	require.NoError(t, err)
	defer func() { assert.NoError(t, c.Close()) }()

	require.NoError(t, c.Ping(ctx))
	require.NoError(t, c.Set(ctx, "env:key", []byte("v"), 0))
	defer func() { assert.NoError(t, c.Delete(ctx, "env:key")) }()
	v, err := c.Get(ctx, "env:key")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), v)
}

func TestKeyLimits(t *testing.T) {
	c := newCache(t)
	defer func() { assert.NoError(t, c.Close()) }()

	okKey := strings.Repeat("k", 250)
	require.NoError(t, c.Set(ctx, okKey, []byte("v"), 0))
	defer func() { assert.NoError(t, c.Delete(ctx, okKey)) }()
	_, err := c.Get(ctx, okKey)
	assert.NoError(t, err, "a 250 byte key is allowed")

	for name, key := range map[string]string{
		"too long":      strings.Repeat("k", 251),
		"has a space":   "has space",
		"control chars": "bad\nkey",
	} {
		assert.ErrorIs(t, c.Set(ctx, key, []byte("v"), 0), memcache.ErrMalformedKey, name)
		_, err := c.Get(ctx, key)
		assert.ErrorIs(t, err, memcache.ErrMalformedKey, name)
	}
}

func TestSetIsSharedAcrossConnections(t *testing.T) {
	a, b := newCache(t), newCache(t)
	defer func() { assert.NoError(t, a.Close()) }()
	defer func() { assert.NoError(t, b.Close()) }()

	require.NoError(t, a.Set(ctx, "shared", []byte("from-a"), 0))
	defer func() { assert.NoError(t, a.Delete(ctx, "shared")) }()
	v, err := b.Get(ctx, "shared")
	require.NoError(t, err)
	assert.Equal(t, []byte("from-a"), v)
	assert.NoError(t, b.Delete(ctx, "shared"))
	_, err = a.Get(ctx, "shared")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func TestKeyInjectionIsRejected(t *testing.T) {
	c := newCache(t)
	defer func() { assert.NoError(t, c.Close()) }()
	require.NoError(t, c.Set(ctx, "injection-canary", []byte("canary"), 0))
	defer func() { assert.NoError(t, c.Delete(ctx, "injection-canary")) }()
	defer func() { assert.NoError(t, c.Delete(ctx, "injected")) }()

	for name, key := range map[string]string{
		"extra set":    "x\r\nset injected 0 0 1\r\nz\r\n",
		"extra delete": "x\r\ndelete injection-canary\r\n",
	} {
		assert.ErrorIs(t, c.Set(ctx, key, []byte("v"), 0), memcache.ErrMalformedKey, name)
		_, err := c.Get(ctx, key)
		assert.ErrorIs(t, err, memcache.ErrMalformedKey, name)
	}
	_, err := c.Get(ctx, "injected")
	assert.ErrorIs(t, err, cache.ErrNotFound, "the injected set was not run")
	v, err := c.Get(ctx, "injection-canary")
	require.NoError(t, err, "the injected delete was not run")
	assert.Equal(t, []byte("canary"), v)
}

func TestDeleteMalformedKeyIsAnError(t *testing.T) {
	c := newCache(t)
	defer func() { assert.NoError(t, c.Close()) }()

	for name, key := range map[string]string{
		"too long":      strings.Repeat("k", 251),
		"has a space":   "has space",
		"control chars": "bad\nkey",
	} {
		assert.ErrorIs(t, c.Delete(ctx, key), memcache.ErrMalformedKey, name)
	}
}

func TestSetDeletePingOnClosedPort(t *testing.T) {
	c := memcached.New([]string{closedAddr(t)}, memcached.WithTimeout(2*time.Second))
	defer func() { assert.NoError(t, c.Close()) }()

	start := time.Now()
	assert.Error(t, c.Set(ctx, "k", []byte("v"), 0))
	assert.Error(t, c.Delete(ctx, "k"), "an unreachable server is not a missing key")
	assert.Error(t, c.Ping(ctx))
	assert.Less(t, time.Since(start), 5*time.Second, "each call fails within its 2s timeout")
}

func TestGetOnClosedPortIsNotAMiss(t *testing.T) {
	c := memcached.New([]string{closedAddr(t)}, memcached.WithTimeout(2*time.Second))
	defer func() { assert.NoError(t, c.Close()) }()

	start := time.Now()
	_, err := c.Get(ctx, "k")
	require.Error(t, err)
	assert.NotErrorIs(t, err, cache.ErrNotFound, "an unreachable server is an error, not a miss")
	assert.Less(t, time.Since(start), 5*time.Second, "the call fails within its 2s timeout")
}

func TestOversizedValueIsRejected(t *testing.T) {
	c := newCache(t)
	defer func() { assert.NoError(t, c.Close()) }()
	defer func() { assert.NoError(t, c.Delete(ctx, "oversize")) }()
	defer func() { assert.NoError(t, c.Delete(ctx, "oversize-old")) }()
	big := bytes.Repeat([]byte("x"), 2<<20)

	err := c.Set(ctx, "oversize", big, 0)
	require.Error(t, err, "a 2 MiB value is over the item limit")
	assert.ErrorContains(t, err, "too large")
	_, err = c.Get(ctx, "oversize")
	assert.ErrorIs(t, err, cache.ErrNotFound, "the rejected value was not stored")

	require.NoError(t, c.Set(ctx, "oversize-old", []byte("v1"), 0))
	require.Error(t, c.Set(ctx, "oversize-old", big, 0))
	_, err = c.Get(ctx, "oversize-old")
	assert.ErrorIs(t, err, cache.ErrNotFound, "the failed overwrite also removed the old value")
}

type proxy struct {
	l     net.Listener
	mu    sync.Mutex
	conns []net.Conn
	once  sync.Once
}

func startProxy(t *testing.T, target string) *proxy {
	t.Helper()
	return startProxyAt(t, "127.0.0.1:0", target)
}

func startProxyAt(t *testing.T, addr, target string) *proxy {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	p := &proxy{l: l}
	go func() {
		for {
			client, err := l.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", target)
			if err != nil {
				_ = client.Close()
				continue
			}
			p.mu.Lock()
			p.conns = append(p.conns, client, server)
			p.mu.Unlock()
			go func() { _, _ = io.Copy(server, client); _ = server.Close() }()
			go func() { _, _ = io.Copy(client, server); _ = client.Close() }()
		}
	}()
	return p
}

func (p *proxy) addr() string { return p.l.Addr().String() }

func (p *proxy) stop() {
	p.once.Do(func() {
		_ = p.l.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, c := range p.conns {
			_ = c.Close()
		}
	})
}

func TestRecoversAfterServerComesBack(t *testing.T) {
	const key = "recovery-key"
	direct := newCache(t)
	defer func() { assert.NoError(t, direct.Close()) }()
	defer func() { assert.NoError(t, direct.Delete(ctx, key)) }()

	p := startProxy(t, MemcachedHost)
	defer p.stop()
	addr := p.addr()
	c := memcached.New([]string{addr}, memcached.WithTimeout(2*time.Second))
	defer func() { assert.NoError(t, c.Close()) }()

	require.NoError(t, c.Set(ctx, key, []byte("v"), 0))
	p.stop()
	_, err := c.Get(ctx, key)
	require.Error(t, err, "the server is gone")
	assert.NotErrorIs(t, err, cache.ErrNotFound)

	back := startProxyAt(t, addr, MemcachedHost)
	defer back.stop()
	assert.Eventually(t, func() bool {
		v, err := c.Get(ctx, key)
		return err == nil && string(v) == "v"
	}, 5*time.Second, 100*time.Millisecond, "the client reads the key again once the server is back")
}

func hungAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return l.Addr().String()
}

func TestGetOnHungServerReturnsAnError(t *testing.T) {
	c := memcached.New([]string{hungAddr(t)}, memcached.WithTimeout(2*time.Second))
	defer func() { assert.NoError(t, c.Close()) }()

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := c.Get(ctx, "k")
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
		assert.NotErrorIs(t, err, cache.ErrNotFound, "a hung server is an error, not a miss")
		assert.Less(t, time.Since(start), 5*time.Second, "the call fails at its 2s timeout")
	case <-time.After(10 * time.Second):
		t.Fatal("Get blocked on a server that never replies")
	}
}

func TestThirtyDayTTLIsStored(t *testing.T) {
	c := newCache(t)
	defer func() { assert.NoError(t, c.Close()) }()
	const key = "thirty-day-ttl"
	const thirtyDays = 30 * 24 * 60 * 60

	require.NoError(t, c.Set(ctx, key, []byte("v"), thirtyDays*time.Second))
	defer func() { assert.NoError(t, c.Delete(ctx, key)) }()

	exp, la := metadumpValue(t, key, "exp"), metadumpValue(t, key, "la")
	assert.InDelta(t, float64(thirtyDays), float64(exp-la), 5, "the stored ttl is 30 days")
}

func metadumpValue(t *testing.T, key, field string) int64 {
	t.Helper()
	lines, err := metadump()
	require.NoError(t, err)
	for _, line := range lines {
		fields := strings.Fields(line)
		if fields[0] != "key="+key {
			continue
		}
		for _, f := range fields[1:] {
			if v, ok := strings.CutPrefix(f, field+"="); ok {
				n, err := strconv.ParseInt(v, 10, 64)
				require.NoError(t, err)
				return n
			}
		}
		t.Fatalf("the metadump line for %q has no %s= field", key, field)
	}
	t.Fatalf("key %q is not in the server's metadump", key)
	return 0
}

func TestTTLBeyond2038IsClamped(t *testing.T) {
	c := newCache(t)
	defer func() { assert.NoError(t, c.Close()) }()
	const key = "clamped-ttl"

	require.NoError(t, c.Set(ctx, key, []byte("v"), 100*365*24*time.Hour))
	defer func() { assert.NoError(t, c.Delete(ctx, key)) }()

	assert.EqualValues(t, math.MaxInt32, metadumpValue(t, key, "exp"), "the expiry is clamped to 2147483647")
}

func TestOpen_TimeoutOptionIsApplied(t *testing.T) {
	c, err := cache.Open(ctx, "memcached://"+hungAddr(t)+"?timeout=300ms")
	require.NoError(t, err)
	defer func() { assert.NoError(t, c.Close()) }()

	start := time.Now()
	_, err = c.Get(ctx, "k")
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.GreaterOrEqual(t, elapsed, 250*time.Millisecond, "the call waited for timeout=300ms, not the 100ms default")
	assert.Less(t, elapsed, time.Second, "and no longer than that")
}

func TestOpen_ServerOptionWithOneServerDown(t *testing.T) {
	c, err := cache.Open(ctx, "memcached://"+MemcachedHost+"?server="+closedAddr(t)+"&timeout=500ms")
	require.NoError(t, err)
	defer func() { assert.NoError(t, c.Close()) }()
	direct := newCache(t)
	defer func() { assert.NoError(t, direct.Close()) }()

	up, down := 0, 0
	for i := 0; i < 60; i++ {
		key := fmt.Sprintf("spread-%d", i)
		if err := c.Set(ctx, key, []byte("v"), 0); err != nil {
			down++
			_, gerr := c.Get(ctx, key)
			require.Error(t, gerr, key)
			assert.NotErrorIs(t, gerr, cache.ErrNotFound, "%s: a key on the dead server is an error, not a miss", key)
			continue
		}
		up++
		defer func(key string) { assert.NoError(t, direct.Delete(ctx, key)) }(key)
		v, gerr := c.Get(ctx, key)
		require.NoError(t, gerr, key)
		assert.Equal(t, []byte("v"), v, key)
	}
	assert.Positive(t, up, "some keys hash to the live server")
	assert.Positive(t, down, "some keys hash to the dead server")
}

func TestCloseReleasesConnections(t *testing.T) {
	base, err := serverStat("curr_connections")
	require.NoError(t, err)

	clients := make([]*memcached.Client, 10)
	for i := range clients {
		clients[i] = newCache(t)
		require.NoError(t, clients[i].Ping(ctx))
	}
	assert.Eventually(t, func() bool {
		n, err := serverStat("curr_connections")
		return err == nil && n >= base+10
	}, 3*time.Second, 50*time.Millisecond, "ten open clients hold at least ten connections")

	for _, c := range clients {
		assert.NoError(t, c.Close())
	}
	assert.Eventually(t, func() bool {
		n, err := serverStat("curr_connections")
		return err == nil && n <= base
	}, 3*time.Second, 50*time.Millisecond, "after Close the server's connection count is back to the start")
}
