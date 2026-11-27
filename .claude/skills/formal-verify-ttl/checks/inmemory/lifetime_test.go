package inmemory

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	keys     = 2
	opsPerKe = 5
	nOps     = keys*opsPerKe + 3
	maxLen   = 6
)

type slot struct {
	present  bool
	value    string
	forever  bool
	deadline int
}

type ref struct {
	slots [keys]slot
	now   int
}

func (r *ref) live(k int) bool {
	s := r.slots[k]
	return s.present && (s.forever || r.now < s.deadline)
}

func opName(op int) string {
	if op >= keys*opsPerKe {
		return [...]string{"tick1s", "tick2s", "sweep"}[op-keys*opsPerKe]
	}
	k, kind := op/opsPerKe, op%opsPerKe
	return fmt.Sprintf("%s(k%d)", [...]string{"set(ttl=0)", "set(ttl=1s)", "set(ttl=2s)", "set(ttl=-1s)", "delete"}[kind], k)
}

func TestFormalEveryOperationSequence(t *testing.T) {
	var clockSecs int
	c := New(WithCleanupInterval(time.Hour))
	defer func() { _ = c.Close() }()
	c.now = func() time.Time { return time.Unix(1000+int64(clockSecs), 0) }

	total := 0
	for length := 1; length <= maxLen; length++ {
		span := 1
		for i := 0; i < length; i++ {
			span *= nOps
		}
		for n := 0; n < span; n++ {
			total++
			c.mu.Lock()
			clear(c.data)
			c.mu.Unlock()
			clockSecs = 0
			var r ref
			var trace []string
			rest := n
			for step := 0; step < length; step++ {
				op := rest % nOps
				rest /= nOps
				trace = append(trace, opName(op))
				val := fmt.Sprintf("v%d", step)
				switch {
				case op == keys*opsPerKe:
					clockSecs++
					r.now++
				case op == keys*opsPerKe+1:
					clockSecs += 2
					r.now += 2
				case op == keys*opsPerKe+2:
					c.sweep()
				default:
					k, kind := op/opsPerKe, op%opsPerKe
					key := fmt.Sprintf("k%d", k)
					switch kind {
					case 0:
						c.Set(key, val, 0)
						r.slots[k] = slot{present: true, value: val, forever: true}
					case 1:
						c.Set(key, val, time.Second)
						r.slots[k] = slot{present: true, value: val, deadline: r.now + 1}
					case 2:
						c.Set(key, val, 2*time.Second)
						r.slots[k] = slot{present: true, value: val, deadline: r.now + 2}
					case 3:
						c.Set(key, val, -time.Second)
						r.slots[k] = slot{present: true, value: val, forever: true}
					case 4:
						c.Delete(key)
						r.slots[k] = slot{}
					}
				}
				if msg := compare(c, &r); msg != "" {
					t.Fatalf("after %s: %s", strings.Join(trace, ", "), msg)
				}
				if op == keys*opsPerKe+2 {
					if msg := leftovers(c, &r); msg != "" {
						t.Fatalf("after %s: %s", strings.Join(trace, ", "), msg)
					}
				}
			}
		}
	}
	t.Logf("checked %d operation sequences of length 1 to %d over %d operations", total, maxLen, nOps)
}

func compare(c *Client, r *ref) string {
	for k := 0; k < keys; k++ {
		key := fmt.Sprintf("k%d", k)
		got, ok := c.Get(key)
		switch {
		case r.live(k) && (!ok || got != r.slots[k].value):
			return fmt.Sprintf("Get(%s) = %q, %v but the value %q should still be live", key, got, ok, r.slots[k].value)
		case !r.live(k) && ok:
			return fmt.Sprintf("Get(%s) = %q, true but the entry should be gone", key, got)
		}
	}
	return ""
}

func leftovers(c *Client, r *ref) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for k := 0; k < keys; k++ {
		key := fmt.Sprintf("k%d", k)
		if _, held := c.data[key]; held && !r.live(k) {
			return fmt.Sprintf("sweep left the expired entry %s in memory", key)
		}
	}
	return ""
}
