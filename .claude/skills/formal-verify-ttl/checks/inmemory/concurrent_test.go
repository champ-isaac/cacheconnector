//go:build race

package inmemory

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Check 5: Get and Set run at the same time as sweep. It is built only under -race, because a
// locking mistake in sweep (a read lock where it deletes) is a data race that the race detector
// reports whenever the accesses overlap, and a plain run sees only by luck.
//
// The janitor is left idle (an interval of one hour). One goroutine runs sweep in a loop, and
// the check also holds that, while this goes on, a key that never expires is always found with
// its value, and a value that is found is the one that was stored.
func TestFormalConcurrentGetSetSweep(t *testing.T) {
	const (
		liveKeys = 8
		setters  = 3
		readers  = 4
		rounds   = 4000
	)
	var secs atomic.Int64
	clock := func(c *Client) { c.now = func() time.Time { return time.Unix(1000+secs.Load(), 0) } }
	c := New(clock, WithCleanupInterval(time.Hour))
	defer func() { _ = c.Close() }()

	for k := 0; k < liveKeys; k++ {
		c.Set(fmt.Sprintf("live-%d", k), "stay", 0)
	}

	var bad atomic.Value // first wrong observation, as text
	fail := func(format string, args ...any) { bad.CompareAndSwap(nil, fmt.Sprintf(format, args...)) }

	var stop atomic.Bool
	var background sync.WaitGroup
	background.Add(1 + readers)
	go func() { // sweep, over and over, while the others run
		defer background.Done()
		for !stop.Load() {
			c.sweep()
		}
	}()
	for r := 0; r < readers; r++ {
		go func() {
			defer background.Done()
			for i := 0; !stop.Load(); i++ {
				live := fmt.Sprintf("live-%d", i%liveKeys)
				if v, ok := c.Get(live); !ok || v != "stay" {
					fail("Get(%q) = %q, %v while sweep was running, want \"stay\", true", live, v, ok)
				}
				if v, ok := c.Get(fmt.Sprintf("tmp-%d", i%64)); ok && v != "tmp" {
					fail("Get of a tmp key returned %q, want \"tmp\"", v)
				}
			}
		}()
	}

	var writers sync.WaitGroup
	writers.Add(setters)
	for s := 0; s < setters; s++ {
		go func() {
			defer writers.Done()
			for i := 0; i < rounds; i++ {
				c.Set(fmt.Sprintf("tmp-%d", i%64), "tmp", time.Second) // expires once the clock moves on
				c.Set(fmt.Sprintf("live-%d", i%liveKeys), "stay", 0)
				if i%25 == 0 {
					secs.Add(2) // makes the tmp keys written so far expire, so sweep has work
				}
			}
		}()
	}
	writers.Wait()
	stop.Store(true)
	background.Wait()

	if msg, _ := bad.Load().(string); msg != "" {
		t.Fatal(msg)
	}

	secs.Add(10)
	c.sweep()
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k := 0; k < liveKeys; k++ {
		if _, ok := c.data[fmt.Sprintf("live-%d", k)]; !ok {
			t.Fatalf("live-%d was lost", k)
		}
	}
	for k, e := range c.data {
		if e.expired(c.now()) {
			t.Fatalf("%q is expired but still held after a final sweep", k)
		}
	}
	t.Logf("%d setters x %d rounds, %d readers and one sweep loop ran together without a race or a wrong value", setters, rounds, readers)
}
