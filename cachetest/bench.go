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

func Bench(b *testing.B, open func(b *testing.B) cache.Cache) {
	for _, c := range benchCases(open) {
		b.Run(c.name, c.run)
	}
}

type benchCase struct {
	name string
	run  func(b *testing.B)
}

func benchCases(open func(b *testing.B) cache.Cache) []benchCase {
	ctx := context.Background()
	sizes := []struct {
		name string
		n    int
	}{{"64B", 64}, {"1KiB", 1 << 10}, {"64KiB", 64 << 10}}

	cases := []benchCase{
		{"Ping", func(b *testing.B) {
			c := open(b)
			if err := c.Ping(ctx); err != nil {
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
				i := int(next.Add(1)) * 97
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

const benchKeys = 128

func benchSetup(b *testing.B, open func(b *testing.B) cache.Cache, size int, preload bool) (cache.Cache, []string, []byte) {
	b.Helper()
	ctx := context.Background()
	c := open(b)
	keys := make([]string, benchKeys)
	for i := range keys {
		keys[i] = fmt.Sprintf("bench:%s:%d", b.Name(), i)
	}
	val := make([]byte, size)
	for i := range val {
		val[i] = byte(i%251) + 1
	}
	b.Cleanup(func() {
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
