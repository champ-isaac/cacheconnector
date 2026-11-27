package memcached

import (
	"fmt"
	"math"
	"testing"
	"time"
)

const (
	maxDelta = 30 * 24 * 60 * 60
	maxExp   = math.MaxInt32
)

func serverDeadline(exp int32, now int64) int64 {
	switch {
	case exp == 0:
		return -1
	case int64(exp) <= maxDelta:
		return now + int64(exp)
	default:
		return int64(exp)
	}
}

func problem(ttl time.Duration, now int64) string {
	got := expiration(ttl, time.Unix(now, 0))
	if ttl <= 0 {
		if got != 0 {
			return fmt.Sprintf("ttl %v is not positive, so it must mean no expiry (0), got %d", ttl, got)
		}
		return ""
	}
	if got == 0 {
		return fmt.Sprintf("ttl %v is positive but became 0, so the entry would never expire", ttl)
	}
	secs := int64(ttl / time.Second)
	if ttl%time.Second != 0 {
		secs++
	}
	want := now + secs
	have := serverDeadline(got, now)
	if secs <= maxDelta || want <= maxExp {
		if have != want {
			return fmt.Sprintf("ttl %v at now=%d: memcached would expire it at %d, want %d (%+d s)", ttl, now, have, want, have-want)
		}
		return ""
	}
	if have != maxExp {
		return fmt.Sprintf("ttl %v at now=%d is past the int32 ceiling: memcached would expire it at %d, want the ceiling %d", ttl, now, have, int64(maxExp))
	}
	return ""
}

type finder struct {
	t     *testing.T
	found []string
	runs  int
}

func (f *finder) try(ttl time.Duration, now int64) {
	f.runs++
	if p := problem(ttl, now); p != "" && len(f.found) < 5 {
		f.found = append(f.found, p)
	}
}

func (f *finder) done() {
	f.t.Helper()
	f.t.Logf("checked %d (ttl, now) pairs", f.runs)
	for _, p := range f.found {
		f.t.Error(p)
	}
}

var nows = []int64{0, 1_000, 1_700_000_000, 1_795_000_000, 2_000_000_000, 2_147_483_000, 2_147_483_646}

var residues = []time.Duration{0, 1, 500 * time.Millisecond, time.Second - 1}

func TestFormalExpirationEveryWholeSecondUpTo35Days(t *testing.T) {
	f := &finder{t: t}
	for _, now := range nows {
		for s := int64(-5); s <= 3_000_000; s++ {
			for _, r := range residues {
				f.try(time.Duration(s)*time.Second+r, now)
			}
		}
	}
	f.done()
}

func TestFormalExpirationAroundTheBoundaries(t *testing.T) {
	f := &finder{t: t}
	const maxSecs = int64(math.MaxInt64 / int64(time.Second))
	for _, now := range nows {
		centers := []int64{maxDelta, maxExp - now, maxExp - now + maxDelta, maxExp, 1 << 32, maxSecs}
		for _, c := range centers {
			for s := c - 100; s <= c+100; s++ {
				if s < 0 || s > maxSecs {
					continue
				}
				for _, r := range residues {
					ttl := time.Duration(s)*time.Second + r
					if ttl < 0 {
						continue
					}
					f.try(ttl, now)
				}
			}
		}
		for s := int64(0); s <= maxSecs; s += 1_000_003 {
			f.try(time.Duration(s)*time.Second, now)
			f.try(time.Duration(s)*time.Second+1, now)
		}
		for _, ttl := range []time.Duration{math.MaxInt64, math.MaxInt64 - 1, math.MaxInt64 / 2, math.MinInt64, math.MinInt64 + 1} {
			f.try(ttl, now)
		}
	}
	f.done()
}

func FuzzFormalExpiration(f *testing.F) {
	f.Add(int64(time.Second), int64(1_795_000_000))
	f.Add(int64(30*24*time.Hour+1), int64(2_147_483_000))
	f.Fuzz(func(t *testing.T, ttl int64, now int64) {
		if now < 0 {
			now = -(now + 1)
		}
		now %= maxExp
		if p := problem(time.Duration(ttl), now); p != "" {
			t.Fatal(p)
		}
	})
}
