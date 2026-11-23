package all

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
)

func TestAllBackendsRegistered(t *testing.T) {
	assert.Equal(t,
		[]string{"inmemory", "memcached", "redis", "rediss", "valkey", "valkeys"},
		cache.Backends())
}

// Switching the driver in the file and reopening picks a different backend
// from the same compiled binary.
func TestFromFileSwitchesDriver(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("REDIS_ADDR", mr.Addr())
	path := filepath.Join(t.TempDir(), "cache.yaml")
	kind := func(driver string) string {
		require.NoError(t, os.WriteFile(path, []byte("driver: "+driver+"\n"), 0o600))
		c, err := cache.FromFile(context.Background(), path)
		require.NoError(t, err)
		defer func() { require.NoError(t, c.Close()) }()
		return fmt.Sprintf("%T", c)
	}
	assert.NotEqual(t, kind("inmemory"), kind("redis"))
}
