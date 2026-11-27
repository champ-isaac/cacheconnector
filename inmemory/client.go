package inmemory

import (
	"sync"
	"time"
)

type entry struct {
	value   string
	expires time.Time
}

type Client struct {
	mu       sync.RWMutex
	data     map[string]entry
	now      func() time.Time
	interval time.Duration

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

type Option func(*Client)

// WithCleanupInterval sets how often the background janitor removes expired
// entries. A zero or negative d is ignored and the default of one minute stays,
// because time.NewTicker panics on a non-positive interval. Expired entries are
// never returned by Get whatever the interval is.
func WithCleanupInterval(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.interval = d
		}
	}
}

func New(opts ...Option) *Client {
	c := &Client{
		data:     make(map[string]entry),
		now:      time.Now,
		interval: time.Minute,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	go c.janitor()
	return c
}

func (c *Client) Get(key string) (string, bool) {
	c.mu.RLock()
	e, ok := c.data[key]
	c.mu.RUnlock()
	if !ok || e.expired(c.now()) {
		return "", false
	}
	return e.value, true
}

func (c *Client) Set(key, value string, ttl time.Duration) {
	e := entry{value: value}
	if ttl > 0 {
		e.expires = c.now().Add(ttl)
	}
	c.mu.Lock()
	c.data[key] = e
	c.mu.Unlock()
}

func (c *Client) Delete(key string) {
	c.mu.Lock()
	delete(c.data, key)
	c.mu.Unlock()
}

func (c *Client) Close() error {
	c.stopOnce.Do(func() { close(c.stop) })
	<-c.done
	return nil
}

func (e entry) expired(now time.Time) bool {
	return !e.expires.IsZero() && !now.Before(e.expires)
}

func (c *Client) janitor() {
	defer close(c.done)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.sweep()
		}
	}
}

func (c *Client) sweep() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.data {
		if e.expired(now) {
			delete(c.data, k)
		}
	}
}
