package all

import (
	_ "github.com/champ-isaac/cacheconnector/inmemory"
	_ "github.com/champ-isaac/cacheconnector/memcached"
	_ "github.com/champ-isaac/cacheconnector/redis"
	_ "github.com/champ-isaac/cacheconnector/valkey"
)
