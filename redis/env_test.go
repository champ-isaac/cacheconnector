package redis

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromEnv(t *testing.T) {
	t.Run("reads every setting", func(t *testing.T) {
		mr := miniredis.RunT(t)
		mr.RequireUserAuth("u", "p")
		t.Setenv("REDIS_ADDR", mr.Addr())
		t.Setenv("REDIS_USERNAME", "u")
		t.Setenv("REDIS_PASSWORD", "p")
		t.Setenv("REDIS_DB", "3")
		c, err := FromEnv(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, c.Close()) })
		require.NoError(t, c.Set(ctx, "k", "v", 0))
		mr.Select(3)
		got, err := mr.Get("k")
		assert.NoError(t, err)
		assert.Equal(t, "v", got, "REDIS_DB is honoured")
	})
	t.Run("unset keeps the defaults", func(t *testing.T) {
		for _, k := range []string{"REDIS_ADDR", "REDIS_USERNAME", "REDIS_PASSWORD", "REDIS_DB"} {
			t.Setenv(k, "")
		}
		opts, err := OptionsFromEnv()
		require.NoError(t, err)
		assert.Empty(t, opts)
	})
	t.Run("values are trimmed", func(t *testing.T) {
		t.Setenv("REDIS_ADDR", " h:1 ")
		t.Setenv("REDIS_DB", "3\r\n")
		opts, err := OptionsFromEnv()
		require.NoError(t, err)
		assert.Len(t, opts, 2)
	})
	t.Run("bad db", func(t *testing.T) {
		for _, v := range []string{"three", "-1"} {
			t.Setenv("REDIS_DB", v)
			_, err := FromEnv(ctx)
			assert.ErrorContains(t, err, "bad REDIS_DB")
		}
	})
	t.Run("unreachable server", func(t *testing.T) {
		t.Setenv("REDIS_ADDR", "127.0.0.1:1")
		t.Setenv("REDIS_PASSWORD", "hunter2")
		t.Setenv("REDIS_DB", "")
		_, err := FromEnv(ctx)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "hunter2")
	})
}
