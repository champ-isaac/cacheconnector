package inmemory

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var intervals = []time.Duration{
	math.MinInt64, math.MinInt64 + 1, -time.Hour, -time.Second, -1, 0, 1, 2,
	time.Millisecond, time.Second, time.Hour, math.MaxInt64 - 1, math.MaxInt64,
}

func within(limit time.Duration, f func()) bool {
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(limit):
		return false
	}
}

func TestFormalIntervalStaysPositiveUnderAnyOptions(t *testing.T) {
	n := 0
	var walk func(c *Client, want time.Duration, trail []time.Duration)
	walk = func(c *Client, want time.Duration, trail []time.Duration) {
		n++
		if c.interval != want || c.interval <= 0 {
			t.Fatalf("after WithCleanupInterval%v the interval is %v, want %v and never zero or negative", trail, c.interval, want)
		}
		if len(trail) == 3 {
			return
		}
		for _, d := range intervals {
			next := &Client{interval: c.interval}
			WithCleanupInterval(d)(next)
			w := want
			if d > 0 {
				w = d
			}
			walk(next, w, append(append([]time.Duration(nil), trail...), d))
		}
	}
	walk(&Client{interval: time.Minute}, time.Minute, nil)
	t.Logf("checked %d option sequences of length 0 to 3 over %d intervals", n, len(intervals))
}

func TestFormalNewSurvivesAnyInterval(t *testing.T) {
	cases := []*Client{New()}
	for _, d := range intervals[:len(intervals)-2] {
		cases = append(cases, New(WithCleanupInterval(d)))
	}
	time.Sleep(50 * time.Millisecond)
	for i, c := range cases {
		if c.interval <= 0 {
			t.Fatalf("client %d has interval %v", i, c.interval)
		}
		c.Set("k", "v", time.Minute)
		if v, ok := c.Get("k"); !ok || v != "v" {
			t.Fatalf("client %d: Get after Set = %q, %v", i, v, ok)
		}
		if !within(5*time.Second, func() { _ = c.Close() }) {
			t.Fatalf("client %d: Close did not return in 5s", i)
		}
	}
	t.Logf("checked New() and %d intervals, the janitor goroutine stayed alive and Close returned", len(cases)-1)
}

func TestFormalJanitorSweepsAndCloseStopsIt(t *testing.T) {
	var secs atomic.Int64
	var stall atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	open := func() { releaseOnce.Do(func() { close(release) }) }
	defer open()
	// While stall is set, the first clock read blocks until release is closed. Only the
	// janitor reads the clock then, in sweep, so this holds the janitor in the middle of a pass.
	clock := func(c *Client) {
		c.now = func() time.Time {
			if stall.Load() {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
			}
			return time.Unix(1000+secs.Load(), 0)
		}
	}
	c := New(clock, WithCleanupInterval(2*time.Millisecond))
	c.Set("soon", "1", time.Second)
	c.Set("later", "2", time.Hour)
	c.Set("never", "3", 0)
	secs.Store(2)

	held := func(k string) bool {
		c.mu.RLock()
		defer c.mu.RUnlock()
		_, ok := c.data[k]
		return ok
	}
	deadline := time.Now().Add(5 * time.Second)
	for held("soon") && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if held("soon") {
		t.Fatal("the janitor did not remove an expired entry within 5s")
	}
	if !held("later") || !held("never") {
		t.Fatal("the janitor removed an entry that has not expired")
	}

	// Stall the janitor inside a sweep. Close must now wait for it, so none of the 8 callers
	// may return until the janitor is released. A Close that returns early is seen here.
	stall.Store(true)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the janitor did not reach the clock within 5s")
	}
	var wg sync.WaitGroup
	returned := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Close(); returned <- struct{}{} }()
	}
	select {
	case <-returned:
		t.Fatal("Close returned while the janitor was still stalled in a sweep")
	case <-time.After(200 * time.Millisecond):
	}
	open()
	if !within(5*time.Second, wg.Wait) {
		t.Fatal("Close from 8 callers at once did not return in 5s after the janitor was released")
	}
	select {
	case <-c.done:
	default:
		t.Fatal("Close returned but the janitor goroutine is still running")
	}
}
