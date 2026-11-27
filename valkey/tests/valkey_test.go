//go:build integration

package valkey

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/cachetest"
)

func TestCache_Contract(t *testing.T) {
	cachetest.Run(t, newCache, nil)
}

func TestFromEnv_UsesValkeyVariables(t *testing.T) {
	t.Setenv("VALKEY_ADDR", ValkeyHost)
	t.Setenv("VALKEY_PASSWORD", ValkeyPassword)
	t.Setenv("VALKEY_DB", strconv.Itoa(ValkeyDB))
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")
	t.Setenv(cache.EnvBackend, "valkey")
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

func TestOpen_WrongPasswordFails(t *testing.T) {
	const wrong = "not-the-password"
	_, err := cache.Open(ctx, "valkey://:"+wrong+"@"+ValkeyHost+"/"+strconv.Itoa(ValkeyDB))
	require.Error(t, err)
	assert.Regexp(t, `WRONGPASS|NOAUTH`, err.Error())
	assert.NotContains(t, err.Error(), wrong)
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

func TestOpen_NoPasswordFails(t *testing.T) {
	_, err := cache.Open(ctx, "valkey://"+ValkeyHost+"/"+strconv.Itoa(ValkeyDB))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOAUTH")
}

func TestOpen_DatabaseOutOfRangeFails(t *testing.T) {
	_, err := cache.Open(ctx, "valkey://:"+ValkeyPassword+"@"+ValkeyHost+"/999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB index is out of range")
}

func TestArbitraryKeysRoundTrip(t *testing.T) {
	c := newCache(t)
	defer func() { assert.NoError(t, c.Close()) }()
	require.NoError(t, c.Set(ctx, "keys-canary", []byte("canary"), 0))
	defer func() { assert.NoError(t, c.Delete(ctx, "keys-canary")) }()

	for name, key := range map[string]string{
		"space":      "has space",
		"newline":    "bad\nkey",
		"crlf+del":   "k\r\nDEL keys-canary\r\n",
		"1000 bytes": strings.Repeat("k", 1000),
		"utf-8":      "ключ-🔑",
		"empty":      "",
	} {
		defer func(key string) { assert.NoError(t, c.Delete(ctx, key)) }(key)
		require.NoError(t, c.Set(ctx, key, []byte(name), 0), name)
		v, err := c.Get(ctx, key)
		require.NoError(t, err, name)
		assert.Equal(t, []byte(name), v, name)
	}
	v, err := c.Get(ctx, "keys-canary")
	require.NoError(t, err, "the CRLF key did not run its DEL")
	assert.Equal(t, []byte("canary"), v)
}

func TestOpen_UsernameInDSN(t *testing.T) {
	c, err := cache.Open(ctx, "valkey://default:"+ValkeyPassword+"@"+ValkeyHost+"/"+strconv.Itoa(ValkeyDB))
	require.NoError(t, err)
	defer func() { assert.NoError(t, c.Close()) }()
	assert.NoError(t, c.Ping(ctx))

	_, err = cache.Open(ctx, "valkey://nobody:"+ValkeyPassword+"@"+ValkeyHost+"/"+strconv.Itoa(ValkeyDB))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WRONGPASS")
}

func TestFromEnv_UsesValkeyUsername(t *testing.T) {
	t.Setenv("VALKEY_ADDR", ValkeyHost)
	t.Setenv("VALKEY_PASSWORD", ValkeyPassword)
	t.Setenv("VALKEY_DB", strconv.Itoa(ValkeyDB))
	t.Setenv("REDIS_USERNAME", "nobody")
	t.Setenv(cache.EnvBackend, "valkey")
	t.Setenv(cache.EnvURL, "")

	t.Run("default user", func(t *testing.T) {
		t.Setenv("VALKEY_USERNAME", "default")
		c, err := cache.FromEnv(ctx)
		require.NoError(t, err)
		defer func() { assert.NoError(t, c.Close()) }()
		assert.NoError(t, c.Ping(ctx))
	})

	t.Run("unknown user", func(t *testing.T) {
		t.Setenv("VALKEY_USERNAME", "nobody")
		_, err := cache.FromEnv(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "WRONGPASS")
	})
}

func connectedClients() (int, error) {
	info, err := client.Raw().Info(ctx, "clients").Result()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(info, "\r\n") {
		if v, ok := strings.CutPrefix(line, "connected_clients:"); ok {
			return strconv.Atoi(strings.TrimSpace(v))
		}
	}
	return 0, errors.New("connected_clients not found in INFO clients")
}

func TestCloseReleasesConnections(t *testing.T) {
	base, err := connectedClients()
	require.NoError(t, err)

	caches := make([]cache.Cache, 10)
	for i := range caches {
		caches[i] = newCache(t)
		require.NoError(t, caches[i].Ping(ctx))
	}
	assert.Eventually(t, func() bool {
		n, err := connectedClients()
		return err == nil && n >= base+10
	}, 3*time.Second, 50*time.Millisecond, "ten open caches hold at least ten connections")

	for _, c := range caches {
		assert.NoError(t, c.Close())
	}
	assert.Eventually(t, func() bool {
		n, err := connectedClients()
		return err == nil && n <= base
	}, 3*time.Second, 50*time.Millisecond, "after Close the server's client count is back to the start")
}

func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

func TestOpen_ClosedPortIsNotAMiss(t *testing.T) {
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	start := time.Now()
	c, err := cache.Open(tctx, "valkey://:"+ValkeyPassword+"@"+closedAddr(t)+"/"+strconv.Itoa(ValkeyDB))
	if err == nil {
		_ = c.Close()
		t.Fatal("opening a closed port returned no error")
	}
	assert.NotErrorIs(t, err, cache.ErrNotFound, "an unreachable server is an error, not a miss")
	assert.Contains(t, err.Error(), "connection refused")
	assert.Less(t, time.Since(start), 10*time.Second, "the call fails within the context deadline")
}

type proxy struct {
	l      net.Listener
	mu     sync.Mutex
	conns  []net.Conn
	once   sync.Once
	frozen atomic.Bool
}

func (p *proxy) freeze() { p.frozen.Store(true) }

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
			go func() {
				buf := make([]byte, 32<<10)
				for {
					n, err := client.Read(buf)
					if n > 0 && !p.frozen.Load() {
						if _, werr := server.Write(buf[:n]); werr != nil {
							break
						}
					}
					if err != nil {
						break
					}
				}
				_ = server.Close()
			}()
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

func TestGetAfterServerGoesAwayIsNotAMiss(t *testing.T) {
	const key = "outage-key"
	direct := newCache(t)
	defer func() { assert.NoError(t, direct.Close()) }()
	defer func() { assert.NoError(t, direct.Delete(ctx, key)) }()

	p := startProxy(t, ValkeyHost)
	defer p.stop()
	c, err := cache.Open(ctx, "valkey://:"+ValkeyPassword+"@"+p.addr()+"/"+strconv.Itoa(ValkeyDB))
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	require.NoError(t, c.Set(ctx, key, []byte("v"), 0))
	v, err := c.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), v)

	p.stop()
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	start := time.Now()
	_, err = c.Get(tctx, key)
	require.Error(t, err)
	assert.NotErrorIs(t, err, cache.ErrNotFound, "a lost server is an error, not a miss")
	assert.Less(t, time.Since(start), 10*time.Second, "the call fails on its own, before the 15s deadline")
}

func TestRecoversAfterServerComesBack(t *testing.T) {
	const key = "recovery-key"
	direct := newCache(t)
	defer func() { assert.NoError(t, direct.Close()) }()
	defer func() { assert.NoError(t, direct.Delete(ctx, key)) }()

	p := startProxy(t, ValkeyHost)
	defer p.stop()
	addr := p.addr()
	c, err := cache.Open(ctx, "valkey://:"+ValkeyPassword+"@"+addr+"/"+strconv.Itoa(ValkeyDB))
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	require.NoError(t, c.Set(ctx, key, []byte("v"), 0))
	p.stop()
	_, err = c.Get(ctx, key)
	require.Error(t, err, "the server is gone")
	assert.NotErrorIs(t, err, cache.ErrNotFound)

	back := startProxyAt(t, addr, ValkeyHost)
	defer back.stop()
	assert.Eventually(t, func() bool {
		v, err := c.Get(ctx, key)
		return err == nil && string(v) == "v"
	}, 5*time.Second, 100*time.Millisecond, "the client reads the key again once the server is back")
}

func TestGetHonoursContextDeadlineOnFrozenLink(t *testing.T) {
	const key = "frozen-key"
	direct := newCache(t)
	defer func() { assert.NoError(t, direct.Close()) }()
	defer func() { assert.NoError(t, direct.Delete(ctx, key)) }()

	p := startProxy(t, ValkeyHost)
	defer p.stop()
	c, err := cache.Open(ctx, "valkey://:"+ValkeyPassword+"@"+p.addr()+"/"+strconv.Itoa(ValkeyDB))
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	require.NoError(t, c.Set(ctx, key, []byte("v"), 0))
	v, err := c.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), v)

	p.freeze()
	tctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.Get(tctx, key)
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotErrorIs(t, err, cache.ErrNotFound, "a deadline is an error, not a miss")
	assert.Less(t, elapsed, time.Second, "the call ends at its 400ms deadline, not at the client's 2s read timeout")
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

func TestOpen_HungServerReturnsAnError(t *testing.T) {
	addr := hungAddr(t)
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	type result struct {
		c   cache.Cache
		err error
	}
	start := time.Now()
	done := make(chan result, 1)
	go func() {
		c, err := cache.Open(tctx, "valkey://:"+ValkeyPassword+"@"+addr+"/"+strconv.Itoa(ValkeyDB))
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		if r.err == nil {
			_ = r.c.Close()
			t.Fatal("opening a hung server returned no error")
		}
		assert.NotErrorIs(t, r.err, cache.ErrNotFound)
		assert.Less(t, time.Since(start), 10*time.Second, "the call fails at the client's timeout, before the 15s deadline")
	case <-time.After(20 * time.Second):
		t.Fatal("Open blocked on a server that never replies")
	}
}

func TestThirtyDayTTLIsStored(t *testing.T) {
	c := newCache(t)
	defer func() { assert.NoError(t, c.Close()) }()
	const key = "thirty-day-ttl"
	const thirtyDays = 30 * 24 * time.Hour

	require.NoError(t, c.Set(ctx, key, []byte("v"), thirtyDays))
	defer func() { assert.NoError(t, c.Delete(ctx, key)) }()

	ttl, err := client.Raw().TTL(ctx, key).Result()
	require.NoError(t, err)
	assert.InDelta(t, thirtyDays.Seconds(), ttl.Seconds(), 5, "the stored ttl is 30 days")
}
