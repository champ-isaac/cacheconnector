package redis

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	cache "github.com/champ-isaac/cacheconnector"
)

func init() {
	cache.Register("redis", Factory("REDIS", false), cache.WithTLS("rediss"))
	cache.Register("rediss", Factory("REDIS", true))
}

func NewCache(c *Client) cache.Cache { return adapter{c} }

func Factory(envPrefix string, tls bool) cache.Factory {
	return func(ctx context.Context, u *url.URL) (cache.Cache, error) {
		opts, err := OptionsFromURL(u, envPrefix, tls)
		if err != nil {
			return nil, err
		}
		c, err := New(ctx, opts...)
		if err != nil {
			return nil, err
		}
		return NewCache(c), nil
	}
}

func OptionsFromURL(u *url.URL, envPrefix string, tls bool) ([]Option, error) {
	if u.Host == "" {
		if tls {
			return nil, errors.New("redis: a TLS DSN needs a host")
		}
		return OptionsFromEnvPrefix(envPrefix)
	}
	opts := []Option{WithAddr(u.Host)}
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			opts = append(opts, WithUsername(name))
		}
		if pw, ok := u.User.Password(); ok && pw != "" {
			opts = append(opts, WithPassword(pw))
		}
	}
	if db := strings.Trim(u.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("redis: bad database %q in DSN", db)
		}
		opts = append(opts, WithDB(n))
	}
	if tls {
		q := u.Query()
		servername := q.Get("servername")
		if servername == "" {
			servername = u.Hostname()
		}
		opts = append(opts, WithEnableTls(true), WithCACertPath(q.Get("ca_cert")), WithServerName(servername))
	}
	return opts, nil
}

type adapter struct{ c *Client }

func (a adapter) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := a.c.raw.Get(ctx, key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, cache.ErrNotFound
	}
	return v, err
}

func (a adapter) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl < 0 {
		ttl = 0
	}
	return a.c.raw.Set(ctx, key, value, ttl).Err()
}

func (a adapter) Delete(ctx context.Context, key string) error { return a.c.raw.Del(ctx, key).Err() }
func (a adapter) Ping(ctx context.Context) error               { return a.c.raw.Ping(ctx).Err() }
func (a adapter) Close() error                                 { return a.c.Close() }
