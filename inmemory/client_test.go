package inmemory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestClient(t *testing.T) (*Client, *clock) {
	t.Helper()
	clk := &clock{t: time.Unix(1000, 0)}
	c := New(WithCleanupInterval(time.Hour))
	c.now = clk.now
	t.Cleanup(func() { _ = c.Close() })
	return c, clk
}

func TestClient_SetGetDelete(t *testing.T) {
	c, _ := newTestClient(t)

	_, ok := c.Get("missing")
	assert.False(t, ok)

	c.Set("k", "v", 0)
	v, ok := c.Get("k")
	assert.True(t, ok)
	assert.Equal(t, "v", v)

	c.Set("k", "v2", -time.Second)
	v, _ = c.Get("k")
	assert.Equal(t, "v2", v)

	c.Delete("k")
	_, ok = c.Get("k")
	assert.False(t, ok)
	c.Delete("never-existed")
}

func TestClient_Expiry(t *testing.T) {
	c, clk := newTestClient(t)
	c.Set("k", "v", time.Minute)

	clk.t = clk.t.Add(59 * time.Second)
	_, ok := c.Get("k")
	assert.True(t, ok, "still alive just before the ttl")

	clk.t = clk.t.Add(time.Second)
	_, ok = c.Get("k")
	assert.False(t, ok, "expired exactly at the ttl")
}

func TestClient_Sweep(t *testing.T) {
	c, clk := newTestClient(t)
	c.Set("old", "1", time.Second)
	c.Set("forever", "2", 0)
	clk.t = clk.t.Add(time.Minute)

	c.sweep()

	c.mu.RLock()
	defer c.mu.RUnlock()
	assert.NotContains(t, c.data, "old")
	assert.Contains(t, c.data, "forever")
}

func TestClient_JanitorSweepsInBackground(t *testing.T) {
	c := New(WithCleanupInterval(5 * time.Millisecond))
	c.Set("k", "v", time.Millisecond)

	assert.Eventually(t, func() bool {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return len(c.data) == 0
	}, 2*time.Second, 5*time.Millisecond)
	assert.NoError(t, c.Close())
}

func TestClient_CloseTwice(t *testing.T) {
	c := New()
	assert.NoError(t, c.Close())
	assert.NoError(t, c.Close())
}
