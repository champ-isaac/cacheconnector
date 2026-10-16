package utils

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashRing_EmptyRingReturnsNothing(t *testing.T) {
	assert.Equal(t, "", New(3).Get("any"))
}

func TestHashRing_Add_N_Get(t *testing.T) {
	ring := New(100)
	nodes := map[string]bool{"redis-A": true, "redis-B": true, "redis-C": true, "redis-D": true}
	for n := range nodes {
		ring.Add(n)
	}
	assert.Len(t, ring.keys, 400)

	seen := map[string]int{}
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("user%d", i)
		node := ring.Get(key)
		assert.True(t, nodes[node], "key maps to a known node")
		assert.Equal(t, node, ring.Get(key), "same key, same node")
		seen[node]++
	}
	assert.Len(t, seen, 4, "keys spread over all nodes")
}

func TestHashRing_WrapsAround(t *testing.T) {
	ring := New(1)
	ring.Add("only")
	last := ring.keys[len(ring.keys)-1]
	for i := 0; ; i++ {
		key := fmt.Sprintf("probe%d", i)
		if hashKey(key) > last { // past the last slot: must wrap to the first one
			assert.Equal(t, "only", ring.Get(key))
			return
		}
	}
}
