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

	ValkeyDB = 1
)

var (
	ctx    = context.Background()
	client *redis.Client
)

func dsn() string {
	return fmt.Sprintf("valkey://:%s@%s/%d", ValkeyPassword, ValkeyHost, ValkeyDB)
}

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
		redis.WithEnableTLS(false),
	)
	if err != nil {
		panic(err)
	}
	flush()
	exitCode := m.Run()

	flush()
	_ = client.Close()
	os.Exit(exitCode)
}
