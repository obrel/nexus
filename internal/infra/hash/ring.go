package hash

import (
	"hash/fnv"
)

// DefaultShards is the default number of shards for the consistent hash ring.
const DefaultShards = 1024

// Ring maps arbitrary string keys to a fixed number of shards using
// Jump Consistent Hash. It requires no external dependencies and produces
// a provably uniform distribution with O(ln n) time complexity.
type Ring struct {
	numShards int
}

// New creates a Ring with the given number of shards.
// numShards must be >= 1; typical production value is 1024.
func New(numShards int) *Ring {
	if numShards < 1 {
		numShards = DefaultShards
	}
	return &Ring{numShards: numShards}
}

// GetShard maps a groupID to a shard in [0, numShards).
// The mapping is stable: identical inputs always produce the same shard.
func (r *Ring) GetShard(groupID string) int {
	key := fnv64a(groupID)
	return jumpHash(key, r.numShards)
}

// NumShards returns the total number of shards in the ring.
func (r *Ring) NumShards() int {
	return r.numShards
}

// fnv64a hashes a string to a uint64 using FNV-1a, which offers good
// avalanche behaviour and is allocation-free.
func fnv64a(s string) uint64 {
	h := fnv.New64a()
	// fnv.Write never returns an error.
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// jumpHash is a pure Go implementation of Google's Jump Consistent Hash
// (Lamping & Veach, 2014). Given a 64-bit key and bucket count it returns
// a bucket in [0, numBuckets) with uniform probability and no external deps.
func jumpHash(key uint64, numBuckets int) int {
	var b, j int64
	b, j = -1, 0
	for j < int64(numBuckets) {
		b = j
		key = key*2862933555777941757 + 1
		j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((key>>33)+1)))
	}
	return int(b)
}
