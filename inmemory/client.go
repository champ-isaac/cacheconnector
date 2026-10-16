// Package inmemory is a process-local cache with TTL, the same idea as Redis SET with EX.
package inmemory

import (
	"sync"
	"time"
)

type entry struct {
	value   string
	expires time.Time // zero means the entry never expires
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

// WithCleanupInterval sets how often expired entries are swept, default 1 minute.
func WithCleanupInterval(d time.Duration) Option {
	return func(c *Client) { c.interval = d }
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

// Get returns the value of key and false when the key is missing or expired.
func (c *Client) Get(key string) (string, bool) {
	c.mu.RLock()
	e, ok := c.data[key]
	c.mu.RUnlock()
	if !ok || e.expired(c.now()) {
		return "", false
	}
	return e.value, true
}

// Set stores value, a ttl of zero or less means the key never expires.
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

// Close stops the background sweeper, it is safe to call more than once.
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
