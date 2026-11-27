package valkey

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
)

func TestGetHonoursContextDeadlineOnSilentServer(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	c, err := cache.Open(ctx, "valkey://"+mr.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))

	var silent atomic.Bool
	release := make(chan struct{})
	mr.Server().SetPreHook(func(*server.Peer, string, ...string) bool {
		if silent.Load() {
			<-release
		}
		return false
	})

	t.Cleanup(func() { close(release) })

	silent.Store(true)
	deadline, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.Get(deadline, "k")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NotErrorIs(t, err, cache.ErrNotFound, "a timed-out call is an error, not a miss")
	assert.Less(t, elapsed, time.Second, "the call ends at its 400ms deadline, not at the client's 2s read timeout")
}
