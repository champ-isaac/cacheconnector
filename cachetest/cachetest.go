// Package cachetest holds the behaviour every cache.Cache backend must have.
// A backend runs it from its own tests:
//
//	cachetest.Run(t, func(t *testing.T) cache.Cache { ... }, advance)
package cachetest

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
)

// Advance moves the backend's clock forward by d. A backend with a fake clock
// (miniredis, a fake memcached) passes its own; nil makes Run sleep.
type Advance func(d time.Duration)

// Run runs the contract against a fresh cache from newCache for each subtest.
func Run(t *testing.T, newCache func(t *testing.T) cache.Cache, advance Advance) {
	t.Helper()
	if advance == nil {
		advance = time.Sleep
	}
	fresh := func(t *testing.T) cache.Cache {
		t.Helper()
		c := newCache(t)
		t.Cleanup(func() { assert.NoError(t, c.Close()) })
		return c
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
		require.NoError(t, c.Set(ctx, "k", []byte("v"), time.Second))
		_, err := c.Get(ctx, "k")
		require.NoError(t, err, "alive before the ttl passes")
		advance(1500 * time.Millisecond)
		_, err = c.Get(ctx, "k")
		assert.ErrorIs(t, err, cache.ErrNotFound)
	})

	t.Run("ReturnedValueIsACopy", func(t *testing.T) {
		c := fresh(t)
		in := []byte("abc")
		require.NoError(t, c.Set(ctx, "k", in, 0))
		in[0] = 'X' // the caller reuses its buffer after Set
		out, err := c.Get(ctx, "k")
		require.NoError(t, err)
		assert.Equal(t, []byte("abc"), out)
		out[0] = 'Y' // and modifies what Get returned
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
}
