package all

import (
	"testing"

	"github.com/stretchr/testify/assert"

	cache "github.com/champ-isaac/cacheconnector"
)

func TestAllBackendsRegistered(t *testing.T) {
	assert.Equal(t,
		[]string{"inmemory", "memcached", "redis", "rediss", "valkey", "valkeys"},
		cache.Backends())
}
