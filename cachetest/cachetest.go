package cachetest

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
)

type Advance func(d time.Duration)

type scoped struct {
	cache.Cache
	prefix string
	mu     sync.Mutex
	keys   map[string]struct{}
}

func (s *scoped) Get(ctx context.Context, key string) ([]byte, error) {
	return s.Cache.Get(ctx, s.prefix+key)
}

func (s *scoped) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	s.keys[s.prefix+key] = struct{}{}
	s.mu.Unlock()
	return s.Cache.Set(ctx, s.prefix+key, value, ttl)
}

func (s *scoped) Delete(ctx context.Context, key string) error {
	return s.Cache.Delete(ctx, s.prefix+key)
}

func Run(t *testing.T, newCache func(t *testing.T) cache.Cache, advance Advance) {
	t.Helper()
	if advance == nil {
		advance = time.Sleep
	}
	fresh := func(t *testing.T) cache.Cache {
		t.Helper()
		s := &scoped{Cache: newCache(t), prefix: t.Name() + ":", keys: map[string]struct{}{}}
		t.Cleanup(func() { assert.NoError(t, s.Close()) })

		t.Cleanup(func() {
			for key := range s.keys {
				assert.NoError(t, s.Cache.Delete(context.Background(), key))
			}
		})
		return s
	}
	ctx := context.Background()

	t.Run("Ping", func(t *testing.T) {
		assert.NoError(t, fresh(t).Ping(ctx))
	})

	t.Run("GetMissing", func(t *testing.T) {
		_, err := fresh(t).Get(ctx, "missing")
		assert.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("SetGetOverwrite", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))
		v, err := c.Get(ctx, "k")
		require.NoError(t, err)
		assert.Equal(t, []byte("v"), v)

		require.NoError(t, c.Set(ctx, "k", []byte("v2"), time.Hour))
		v, err = c.Get(ctx, "k")
		require.NoError(t, err)
		assert.Equal(t, []byte("v2"), v)
	})

	t.Run("BinaryAndEmptyValues", func(t *testing.T) {
		c := fresh(t)
		bin := []byte{0, 1, 2, '\r', '\n', 255, 0}
		require.NoError(t, c.Set(ctx, "bin", bin, 0))
		got, err := c.Get(ctx, "bin")
		require.NoError(t, err)
		assert.True(t, bytes.Equal(bin, got))

		require.NoError(t, c.Set(ctx, "empty", []byte{}, 0))
		got, err = c.Get(ctx, "empty")
		require.NoError(t, err, "an empty value is not a miss")
		assert.Empty(t, got)
	})

	t.Run("LargeValueRoundTrip", func(t *testing.T) {
		c := fresh(t)
		big := make([]byte, 512<<10)
		for i := range big {
			big[i] = byte(i % 251)
		}
		require.NoError(t, c.Set(ctx, "big", big, 0))
		got, err := c.Get(ctx, "big")
		require.NoError(t, err)
		require.Len(t, got, len(big))
		assert.True(t, bytes.Equal(big, got), "the 512 KiB value read back differs from the one set")
	})

	t.Run("NegativeTTLMeansNoExpiry", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "k", []byte("v"), -time.Second))
		advance(1500 * time.Millisecond)
		_, err := c.Get(ctx, "k")
		assert.NoError(t, err)
	})

	t.Run("Delete", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))
		require.NoError(t, c.Delete(ctx, "k"))
		_, err := c.Get(ctx, "k")
		assert.ErrorIs(t, err, cache.ErrNotFound)
		assert.NoError(t, c.Delete(ctx, "k"), "deleting a missing key is not an error")
	})

	t.Run("TTLExpires", func(t *testing.T) {
		c := fresh(t)

		require.NoError(t, c.Set(ctx, "k", []byte("v"), 3*time.Second))
		require.NoError(t, c.Set(ctx, "long", []byte("v"), time.Hour))
		advance(time.Second)
		_, err := c.Get(ctx, "k")
		require.NoError(t, err, "alive well before the ttl passes")
		advance(2500 * time.Millisecond)
		_, err = c.Get(ctx, "k")
		assert.ErrorIs(t, err, cache.ErrNotFound, "expired after the ttl passed")
		_, err = c.Get(ctx, "long")
		assert.NoError(t, err, "an entry with a long ttl is not expired by the wait")
	})

	t.Run("SubSecondTTLStillExpires", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "short", []byte("v"), 100*time.Millisecond))
		advance(2 * time.Second)
		_, err := c.Get(ctx, "short")
		assert.ErrorIs(t, err, cache.ErrNotFound, "a sub-second ttl expires; it does not become no ttl")
	})

	t.Run("OverwriteWithoutTTLClearsOldTTL", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "k", []byte("v1"), time.Second))
		require.NoError(t, c.Set(ctx, "k", []byte("v2"), 0))
		advance(2 * time.Second)
		v, err := c.Get(ctx, "k")
		require.NoError(t, err, "the overwrite removed the old ttl")
		assert.Equal(t, []byte("v2"), v)
	})

	t.Run("OverwriteWithShortTTLReplacesLongTTL", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "k", []byte("v1"), time.Hour))
		require.NoError(t, c.Set(ctx, "k", []byte("v2"), time.Second))
		advance(2 * time.Second)
		_, err := c.Get(ctx, "k")
		assert.ErrorIs(t, err, cache.ErrNotFound, "the overwrite replaced the old hour-long ttl")
	})

	t.Run("DeleteRemovesOnlyTheExactKey", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "a", []byte("a"), 0))
		require.NoError(t, c.Set(ctx, "ab", []byte("ab"), 0))
		require.NoError(t, c.Delete(ctx, "a"))
		_, err := c.Get(ctx, "a")
		assert.ErrorIs(t, err, cache.ErrNotFound)
		v, err := c.Get(ctx, "ab")
		require.NoError(t, err, "a key that starts with the deleted key is still there")
		assert.Equal(t, []byte("ab"), v)
	})

	t.Run("DeleteExpiredKeyThenSet", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "k", []byte("old"), time.Second))
		advance(2 * time.Second)
		_, err := c.Get(ctx, "k")
		require.ErrorIs(t, err, cache.ErrNotFound, "the key has expired before it is deleted")
		require.NoError(t, c.Delete(ctx, "k"), "deleting an expired key is not an error")
		require.NoError(t, c.Set(ctx, "k", []byte("new"), 0))
		v, err := c.Get(ctx, "k")
		require.NoError(t, err)
		assert.Equal(t, []byte("new"), v)
	})

	t.Run("TTLAroundThirtyDays", func(t *testing.T) {
		c := fresh(t)
		day := 24 * time.Hour
		for _, tc := range []struct {
			key string
			ttl time.Duration
		}{
			{"thirty-days", 30 * day},
			{"thirty-days-and-a-second", 30*day + time.Second},
			{"a-year", 365 * day},
		} {
			require.NoError(t, c.Set(ctx, tc.key, []byte(tc.key), tc.ttl), tc.key)
			v, err := c.Get(ctx, tc.key)
			require.NoError(t, err, "%s: a long ttl is not already expired", tc.key)
			assert.Equal(t, []byte(tc.key), v, tc.key)
		}
	})

	t.Run("VeryLongTTL", func(t *testing.T) {
		c := fresh(t)
		for _, tc := range []struct {
			key string
			ttl time.Duration
		}{
			{"hundred-years", 100 * 365 * 24 * time.Hour},
			{"max-duration", time.Duration(math.MaxInt64)},
		} {
			require.NoError(t, c.Set(ctx, tc.key, []byte(tc.key), tc.ttl), tc.key)
			v, err := c.Get(ctx, tc.key)
			require.NoError(t, err, "%s: a very long ttl does not expire the entry", tc.key)
			assert.Equal(t, []byte(tc.key), v, tc.key)
		}
	})

	t.Run("ReturnedValueIsACopy", func(t *testing.T) {
		c := fresh(t)
		in := []byte("abc")
		require.NoError(t, c.Set(ctx, "k", in, 0))
		in[0] = 'X'
		out, err := c.Get(ctx, "k")
		require.NoError(t, err)
		assert.Equal(t, []byte("abc"), out)
		out[0] = 'Y'
		again, _ := c.Get(ctx, "k")
		assert.Equal(t, []byte("abc"), again)
	})

	t.Run("Concurrent", func(t *testing.T) {
		c := fresh(t)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 25; j++ {
					key := fmt.Sprintf("k%d", j)
					assert.NoError(t, c.Set(ctx, key, []byte("v"), 0))
					if _, err := c.Get(ctx, key); err != nil {
						assert.ErrorIs(t, err, cache.ErrNotFound)
					}
					assert.NoError(t, c.Delete(ctx, key))
				}
			}()
		}
		wg.Wait()
	})

	t.Run("CanceledContext", func(t *testing.T) {
		c := fresh(t)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := c.Get(canceled, "k")
		assert.Error(t, err)
		assert.NotErrorIs(t, err, cache.ErrNotFound, "a canceled call is an error, not a miss")
	})

	t.Run("CanceledSetDoesNotWrite", func(t *testing.T) {
		c := fresh(t)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		assert.Error(t, c.Set(canceled, "k", []byte("v"), 0))
		_, err := c.Get(ctx, "k")
		assert.ErrorIs(t, err, cache.ErrNotFound, "the canceled Set wrote nothing")
	})

	t.Run("CanceledDeleteKeepsTheKey", func(t *testing.T) {
		c := fresh(t)
		require.NoError(t, c.Set(ctx, "k", []byte("v"), 0))
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		assert.Error(t, c.Delete(canceled, "k"))
		v, err := c.Get(ctx, "k")
		require.NoError(t, err, "the canceled Delete did not remove the key")
		assert.Equal(t, []byte("v"), v)
	})

	t.Run("CanceledPing", func(t *testing.T) {
		c := fresh(t)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		assert.Error(t, c.Ping(canceled))
	})
}
