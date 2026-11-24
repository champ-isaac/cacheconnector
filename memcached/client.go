// Package memcached is a cache.Cache backend for Memcached, built on
// github.com/bradfitz/gomemcache. It registers the "memcached" scheme.
//
// DSN: memcached://host:11211. More servers go in repeated server= query
// parameters, and the keys are spread over all of them:
//
//	memcached://10.0.0.1:11211?server=10.0.0.2:11211&timeout=200ms&max_idle_conns=8
//
// A DSN with no host reads MEMCACHED_ADDRS, a comma separated list, default
// 127.0.0.1:11211.
//
// Memcached differs from the other backends in three ways: its TTL has
// 1-second granularity (a ttl is rounded up to whole seconds), its keys are at
// most 250 bytes with no spaces or control characters (such keys fail with
// memcache.ErrMalformedKey), and the client has no context support, so ctx is
// checked before each call but does not interrupt one in flight; use
// WithTimeout to bound a call.
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

// maxRelativeTTL is the largest ttl Memcached reads as "seconds from now"; a
// larger number is read as a Unix timestamp.
const maxRelativeTTL = 30 * 24 * time.Hour

const defaultAddr = "127.0.0.1:11211"

// Client is a Memcached cache.Cache.
type Client struct {
	mc  *memcache.Client
	now func() time.Time
}

// Option configures New.
type Option func(*Client)

// WithTimeout sets the per-call socket timeout, default 100ms.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.mc.Timeout = d }
}

// WithMaxIdleConns sets the idle connections kept per server, default 2.
func WithMaxIdleConns(n int) Option {
	return func(c *Client) { c.mc.MaxIdleConns = n }
}

// New returns a Client for the given servers (host:port). It does not
// connect until the first call; use Ping to check the servers are up.
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

// expiration converts a ttl to Memcached's exptime: 0 for none, else whole
// seconds rounded up, or a Unix timestamp when beyond 30 days.
func expiration(ttl time.Duration, now time.Time) int32 {
	if ttl <= 0 {
		return 0
	}
	// Round up without adding to ttl: ttl + time.Second - 1 overflows for a
	// ttl near the largest Duration.
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
