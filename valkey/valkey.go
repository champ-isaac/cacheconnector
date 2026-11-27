package valkey

import (
	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/redis"
)

func init() {
	cache.Register("valkey", redis.Factory("VALKEY", false), cache.WithTLS("valkeys"))
	cache.Register("valkeys", redis.Factory("VALKEY", true))
}
