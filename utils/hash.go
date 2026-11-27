package utils

import (
	"hash/crc32"
	"sort"
	"strconv"
)

type HashRing struct {
	replicas   int
	hashes     []int
	nodeByHash map[int]string
}

func New(replicas int) *HashRing {
	return &HashRing{
		replicas:   replicas,
		nodeByHash: make(map[int]string),
	}
}

func hashKey(key string) int {
	return int(crc32.ChecksumIEEE([]byte(key)))
}

func (h *HashRing) Add(node string) {
	for i := 0; i < h.replicas; i++ {
		virtualKey := hashKey(node + "#" + strconv.Itoa(i))
		h.hashes = append(h.hashes, virtualKey)
		h.nodeByHash[virtualKey] = node
	}
	sort.Ints(h.hashes)
}

func (h *HashRing) Get(key string) string {
	if len(h.hashes) == 0 {
		return ""
	}
	hash := hashKey(key)
	idx := sort.Search(len(h.hashes), func(i int) bool {
		return h.hashes[i] >= hash
	})
	if idx == len(h.hashes) {
		idx = 0
	}
	return h.nodeByHash[h.hashes[idx]]
}
