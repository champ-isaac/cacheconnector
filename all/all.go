// Package all registers every cache backend: inmemory, redis, valkey and
// memcached. Import it for its side effects when the backend is chosen at run
// time and you accept compiling all the drivers:
//
//	import _ "github.com/champ-isaac/cacheconnector/all"
package all

import (
	_ "github.com/champ-isaac/cacheconnector/inmemory"
	_ "github.com/champ-isaac/cacheconnector/memcached"
	_ "github.com/champ-isaac/cacheconnector/redis"
	_ "github.com/champ-isaac/cacheconnector/valkey"
)
