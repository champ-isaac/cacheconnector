//go:build integration

package valkey

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/redis"
	_ "github.com/champ-isaac/cacheconnector/valkey"
)

const (
	ValkeyHost     = "127.0.0.1:6380"
	ValkeyPassword = "user"
	// ValkeyDB is a database of its own, so the tests can flush it without
	// touching anything else on the server.
	ValkeyDB = 1
)

var (
	ctx    = context.Background()
	client *redis.Client // used only to flush ValkeyDB around the run
)

// dsn is the DSN the tests open. The server is reached through the valkey
// backend, which reuses the Redis client.
func dsn() string {
	return fmt.Sprintf("valkey://:%s@%s/%d", ValkeyPassword, ValkeyHost, ValkeyDB)
}

// newCache opens a new connection to the valkey backend.
func newCache(t *testing.T) cache.Cache {
	t.Helper()
	c, err := cache.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open %s: %v", ValkeyHost, err)
	}
	return c
}

func flush() {
	if err := client.Raw().FlushDB(ctx).Err(); err != nil {
		panic(err)
	}
}

func TestMain(m *testing.M) {
	var err error
	client, err = redis.New(
		ctx,
		redis.WithAddr(ValkeyHost),
		redis.WithUsername("default"),
		redis.WithPassword(ValkeyPassword),
		redis.WithDB(ValkeyDB),
		redis.WithProtocol(2),
		redis.WithReadTimeout(2*time.Second),
		redis.WithWriteTimeout(2*time.Second),
		redis.WithEnableTls(false),
	)
	if err != nil {
		panic(err)
	}
	flush()
	exitCode := m.Run()

	//teardown
	flush()
	_ = client.Close()
	os.Exit(exitCode)
}
