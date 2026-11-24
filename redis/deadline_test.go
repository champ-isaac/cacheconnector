package redis

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
)

// silentProxy forwards TCP connections to target until silence is called. After
// that it reads what a client sends and drops it, so the connections stay open
// and nothing comes back, like a dead network path. miniredis never hangs by
// itself, so this is how a test makes a call wait on the server.
type silentProxy struct {
	l      net.Listener
	silent atomic.Bool
	mu     sync.Mutex
	conns  []net.Conn
}

func startSilentProxy(t *testing.T, target string) *silentProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &silentProxy{l: l}
	t.Cleanup(p.stop)
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
			go func() { // client to server, dropped once silent
				buf := make([]byte, 32<<10)
				for {
					n, err := client.Read(buf)
					if n > 0 && !p.silent.Load() {
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

func (p *silentProxy) addr() string { return p.l.Addr().String() }
func (p *silentProxy) silence()     { p.silent.Store(true) }

func (p *silentProxy) stop() {
	_ = p.l.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
}

// A call's context deadline must end the call even when the server has gone
// silent. go-redis only applies the deadline to the socket when
// ContextTimeoutEnabled is set (see New); without it the call waits for the
// client's read timeout, here 2s, and this test fails on the 1s limit.
func TestGetHonoursContextDeadlineOnSilentLink(t *testing.T) {
	mr := miniredis.RunT(t)
	p := startSilentProxy(t, mr.Addr())
	c, err := New(ctx, WithAddr(p.addr()), WithReadTimeout(2*time.Second), WithWriteTimeout(2*time.Second))
	require.NoError(t, err)
	cc := NewCache(c)
	t.Cleanup(func() { _ = cc.Close() })
	require.NoError(t, cc.Set(ctx, "k", []byte("v"), 0))

	p.silence()
	deadline, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = cc.Get(deadline, "k")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotErrorIs(t, err, cache.ErrNotFound, "a timed-out call is an error, not a miss")
	assert.Less(t, elapsed, time.Second, "the call ends at its 400ms deadline, not at the client's 2s read timeout")
}
