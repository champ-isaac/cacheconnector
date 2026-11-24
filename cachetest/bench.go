package cachetest

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	cache "github.com/champ-isaac/cacheconnector"
)

// Bench is the benchmark counterpart of Run: every backend runs the same
// operations, so the numbers compare. A backend calls it from its own
// benchmarks:
//
//	cachetest.Bench(b, func(b *testing.B) cache.Cache { ... })
//
// open returns a new cache and registers its own Close with b.Cleanup. Each
// sub-benchmark opens its own, so its connection pool is not shared.
//
// The operations are Ping (the cost of one round trip, the floor for every
// network call), Get of a missing key, Get and Set at three value sizes, and a
// parallel Get and Set at 1 KiB. Sequential runs measure one caller waiting on
// each call; for the parallel runs ns/op is the time per operation across all
// callers, the inverse of throughput, not the latency of one call. Values are
// checked on every Get, so a fast wrong answer fails the run.
//
// Keys rotate over a fixed set of 128, so a run does not grow the server or
// hammer one key. Set uses a ttl of one hour, as a real cache would. The keys
// a run wrote are deleted when it ends.
func Bench(b *testing.B, open func(b *testing.B) cache.Cache) {
	for _, c := range benchCases(open) {
		b.Run(c.name, c.run)
	}
}

type benchCase struct {
	name string
	run  func(b *testing.B)
}

// benchCases lists the benchmarks as separate functions, so a test can run each
// on its own: testing.Benchmark reports a failure for one of these but not for
// a benchmark that only has sub-benchmarks.
func benchCases(open func(b *testing.B) cache.Cache) []benchCase {
	ctx := context.Background()
	sizes := []struct {
		name string
		n    int
	}{{"64B", 64}, {"1KiB", 1 << 10}, {"64KiB", 64 << 10}}

	cases := []benchCase{
		{"Ping", func(b *testing.B) {
			c := open(b)
			if err := c.Ping(ctx); err != nil { // connects, so the pool is warm
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.Ping(ctx); err != nil {
					b.Fatal(err)
				}
			}
		}},
		{"GetMiss", func(b *testing.B) {
			c, keys, _ := benchSetup(b, open, 64, false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := c.Get(ctx, keys[i%len(keys)]); !errors.Is(err, cache.ErrNotFound) {
					b.Fatalf("Get of a missing key returned %v, want ErrNotFound", err)
				}
			}
		}},
	}

	for _, s := range sizes {
		cases = append(cases, benchCase{"GetHit/" + s.name, func(b *testing.B) {
			c, keys, val := benchSetup(b, open, s.n, true)
			b.SetBytes(int64(s.n))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := c.Get(ctx, keys[i%len(keys)])
				if err != nil {
					b.Fatal(err)
				}
				if len(got) != s.n || got[0] != val[0] || got[s.n-1] != val[s.n-1] {
					b.Fatalf("Get returned %d bytes that differ from the %d bytes set", len(got), s.n)
				}
			}
		}})
	}

	for _, s := range sizes {
		cases = append(cases, benchCase{"Set/" + s.name, func(b *testing.B) {
			c, keys, val := benchSetup(b, open, s.n, false)
			b.SetBytes(int64(s.n))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := c.Set(ctx, keys[i%len(keys)], val, time.Hour); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			got, err := c.Get(ctx, keys[0])
			if err != nil || len(got) != s.n || got[0] != val[0] {
				b.Fatalf("the value just set did not read back: %d bytes, %v", len(got), err)
			}
		}})
	}

	return append(cases,
		benchCase{"GetHitParallel/1KiB", func(b *testing.B) {
			c, keys, val := benchSetup(b, open, 1<<10, true)
			var next atomic.Int64
			b.SetBytes(1 << 10)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := int(next.Add(1)) * 97 // each caller starts on a different key
				for pb.Next() {
					got, err := c.Get(ctx, keys[i%len(keys)])
					if err != nil || len(got) != len(val) || got[0] != val[0] {
						b.Errorf("Get returned %d bytes, %v", len(got), err)
						return
					}
					i++
				}
			})
		}},
		benchCase{"SetParallel/1KiB", func(b *testing.B) {
			c, keys, val := benchSetup(b, open, 1<<10, false)
			var next atomic.Int64
			b.SetBytes(1 << 10)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := int(next.Add(1)) * 97
				for pb.Next() {
					if err := c.Set(ctx, keys[i%len(keys)], val, time.Hour); err != nil {
						b.Errorf("Set: %v", err)
						return
					}
					i++
				}
			})
		}},
	)
}

// benchKeys is how many keys a benchmark rotates over. 128 keys of the largest
// value, 64 KiB, are 8 MiB, far under the 64 MiB Memcached default, so no key
// is evicted mid-run and turns a hit into a miss. A small set also keeps the
// preload and cleanup short, which a network round trip makes expensive.
const benchKeys = 128

// benchSetup opens a cache and returns it with the keys to rotate over and a
// value of the given size. With preload the keys are already set. The keys are
// deleted at the end.
func benchSetup(b *testing.B, open func(b *testing.B) cache.Cache, size int, preload bool) (cache.Cache, []string, []byte) {
	b.Helper()
	ctx := context.Background()
	c := open(b)
	keys := make([]string, benchKeys) // built here so the timed loop does no formatting
	for i := range keys {
		keys[i] = fmt.Sprintf("bench:%s:%d", b.Name(), i)
	}
	val := make([]byte, size)
	for i := range val {
		val[i] = byte(i%251) + 1 // never zero, so a zeroed or truncated read differs
	}
	b.Cleanup(func() { // runs before the Close that open registered
		for _, k := range keys {
			if err := c.Delete(ctx, k); err != nil {
				b.Logf("cleanup: delete %s: %v", k, err)
				return
			}
		}
	})
	if preload {
		for _, k := range keys {
			if err := c.Set(ctx, k, val, time.Hour); err != nil {
				b.Fatal(err)
			}
		}
	}
	return c, keys, val
}
