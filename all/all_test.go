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

// The real redis and valkey backends declare their TLS schemes through
// cache.WithTLS. The root package holds no pair of its own, so a driver with
// its TLS scheme only passes the agreement check because of that option.
func TestRedisAndValkeyDeclareTLSPairs(t *testing.T) {
	ctx := context.Background()
	for driver, tls := range map[string]string{"redis": "rediss", "valkey": "valkeys"} {
		// A TLS url without a CA cert is rejected by the redis factory. That
		// error means the agreement check passed and the factory was reached.
		_, err := cache.FromConfig(ctx, cache.Config{Driver: driver, URL: tls + "://127.0.0.1:1"})
		require.Error(t, err, driver)
		assert.NotContains(t, err.Error(), "does not match", driver)
		assert.ErrorContains(t, err, "enableTls requires", driver)
	}

	_, err := cache.FromConfig(ctx, cache.Config{Driver: "redis", URL: "valkeys://127.0.0.1:1"})
	assert.ErrorContains(t, err, "does not match the url scheme", "another backend's TLS scheme is not a pair")

	mr := miniredis.RunT(t)
	for _, driver := range []string{"redis", "valkey"} {
		c, err := cache.FromConfig(ctx, cache.Config{Driver: driver, URL: driver + "://" + mr.Addr()})
		require.NoError(t, err, driver)
		require.NoError(t, c.Close())
	}
}
