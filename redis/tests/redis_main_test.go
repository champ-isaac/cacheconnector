//go:build integration

package redis

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/champ-isaac/cacheconnector/redis"
)

const (
	RedisHost = "127.0.0.1:6381"
)

var (
	ctx    = context.Background()
	client *redis.Client
)

func TestMain(m *testing.M) {
	var err error
	client, err = redis.New(
		ctx,
		redis.WithAddr(RedisHost),
		redis.WithUsername("default"),
		redis.WithPassword("user"),
		redis.WithDB(0),
		redis.WithProtocol(2),
		redis.WithPoolSize(100),
		redis.WithMinIdleConns(20),
		redis.WithReadTimeout(2*time.Second),
		redis.WithWriteTimeout(2*time.Second),
		redis.WithEnableTls(false),
	)
	if err != nil {
		panic(err)
	}
	exitCode := m.Run()

	defer func() {
		_ = client.Close()
	}()
	os.Exit(exitCode)
}
