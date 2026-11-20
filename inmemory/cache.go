package inmemory

import (
	"context"
	"fmt"
	"net/url"
	"time"

	cache "github.com/champ-isaac/cacheconnector"
)

func init() { cache.Register("inmemory", factory) }

// NewCache adapts a Client to cache.Cache. Closing the cache closes the Client.
func NewCache(c *Client) cache.Cache { return adapter{c} }

// factory opens "inmemory:", with the optional query cleanup_interval=30s.
func factory(_ context.Context, u *url.URL) (cache.Cache, error) {
	var opts []Option
	if s := u.Query().Get("cleanup_interval"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("inmemory: bad cleanup_interval %q", s)
		}
		opts = append(opts, WithCleanupInterval(d))
	}
	return NewCache(New(opts...)), nil
}

type adapter struct{ c *Client }

func (a adapter) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v, ok := a.c.Get(key)
	if !ok {
		return nil, cache.ErrNotFound
	}
	return []byte(v), nil // string -> []byte copies, so callers own the result
}

func (a adapter) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.c.Set(key, string(value), ttl)
	return nil
}

func (a adapter) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.c.Delete(key)
	return nil
}

func (a adapter) Ping(ctx context.Context) error { return ctx.Err() }
func (a adapter) Close() error                   { return a.c.Close() }
