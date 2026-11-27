package memcached

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	cache "github.com/champ-isaac/cacheconnector"
)

func init() { cache.Register("memcached", factory) }

const maxRelativeTTL = 30 * 24 * time.Hour

const defaultAddr = "127.0.0.1:11211"

type Client struct {
	mc  *memcache.Client
	now func() time.Time
}

type Option func(*Client)

func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.mc.Timeout = d }
}

func WithMaxIdleConns(n int) Option {
	return func(c *Client) { c.mc.MaxIdleConns = n }
}

func New(addrs []string, opts ...Option) *Client {
	if len(addrs) == 0 {
		addrs = []string{defaultAddr}
	}
	c := &Client{mc: memcache.New(addrs...), now: time.Now}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	it, err := c.mc.Get(key)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, cache.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return it.Value, nil
}

func (c *Client) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.mc.Set(&memcache.Item{Key: key, Value: value, Expiration: expiration(ttl, c.now())})
}

func (c *Client) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.mc.Delete(key); err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		return err
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.mc.Ping()
}

func (c *Client) Close() error { return c.mc.Close() }

func expiration(ttl time.Duration, now time.Time) int32 {
	if ttl <= 0 {
		return 0
	}

	secs := int64(ttl / time.Second)
	if ttl%time.Second != 0 {
		secs++
	}
	if ttl > maxRelativeTTL {
		secs += now.Unix()
	}
	if secs > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(secs) // #nosec G115 -- clamped to MaxInt32 above
}

func factory(_ context.Context, u *url.URL) (cache.Cache, error) {
	var addrs []string
	q := u.Query()
	if u.Host != "" {
		addrs = append(addrs, u.Host)
		addrs = append(addrs, q["server"]...)
	} else if env := strings.TrimSpace(os.Getenv("MEMCACHED_ADDRS")); env != "" {
		for _, a := range strings.Split(env, ",") {
			if a = strings.TrimSpace(a); a != "" {
				addrs = append(addrs, a)
			}
		}
	}

	var opts []Option
	if s := q.Get("timeout"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("memcached: bad timeout %q", s)
		}
		opts = append(opts, WithTimeout(d))
	}
	if s := q.Get("max_idle_conns"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("memcached: bad max_idle_conns %q", s)
		}
		opts = append(opts, WithMaxIdleConns(n))
	}
	return New(addrs, opts...), nil
}
