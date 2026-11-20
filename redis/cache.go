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
	cache.Register("redis", Factory("REDIS", false))
	cache.Register("rediss", Factory("REDIS", true))
}

// NewCache adapts a Client to cache.Cache. Closing the cache closes the Client.
func NewCache(c *Client) cache.Cache { return adapter{c} }

// Factory returns the cache.Factory for a Redis-protocol server. The Valkey
// package reuses it, since Valkey speaks the same protocol.
//
// DSN: scheme://[user]:[password]@host:port/db. With tls the scheme is
// "rediss" (or "valkeys") and the query must give ca_cert=/path/ca.pem; it
// may give servername=, which defaults to the host. A DSN with no host, as
// FromEnv builds, reads the <envPrefix>_ADDR, _USERNAME, _PASSWORD and _DB
// environment variables instead. Errors never contain the password.
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

// OptionsFromURL turns a DSN into Options, see Factory for the format.
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
		ttl = 0 // go-redis reserves negative values; the cache contract says no expiry
	}
	return a.c.raw.Set(ctx, key, value, ttl).Err()
}

func (a adapter) Delete(ctx context.Context, key string) error { return a.c.raw.Del(ctx, key).Err() }
func (a adapter) Ping(ctx context.Context) error               { return a.c.raw.Ping(ctx).Err() }
func (a adapter) Close() error                                 { return a.c.Close() }
