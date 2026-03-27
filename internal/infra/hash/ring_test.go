package hash

import (
	"fmt"
	"math"
	"testing"
)

func TestGetShard_InRange(t *testing.T) {
	r := New(DefaultShards)
	cases := []string{
		"group_abc",
		"group_xyz",
		"room:42",
		"",
		"a",
		"very-long-group-id-that-exceeds-normal-length-12345678901234567890",
	}
	for _, id := range cases {
		shard := r.GetShard(id)
		if shard < 0 || shard >= DefaultShards {
			t.Errorf("GetShard(%q) = %d, want [0, %d)", id, shard, DefaultShards)
		}
	}
}

func TestGetShard_Stable(t *testing.T) {
	r := New(DefaultShards)
	ids := []string{"group_a", "group_b", "topic_x", "room:99", ""}
	// Call twice and assert identical results.
	for _, id := range ids {
		first := r.GetShard(id)
		second := r.GetShard(id)
		if first != second {
			t.Errorf("GetShard(%q) unstable: got %d then %d", id, first, second)
		}
	}
}

func TestGetShard_StableAcrossRingInstances(t *testing.T) {
	r1 := New(DefaultShards)
	r2 := New(DefaultShards)
	for i := range 500 {
		id := fmt.Sprintf("group_%d", i)
		if r1.GetShard(id) != r2.GetShard(id) {
			t.Errorf("GetShard(%q) differs between ring instances", id)
		}
	}
}

func TestGetShard_UniformDistribution(t *testing.T) {
	const n = 100_000
	r := New(DefaultShards)
	counts := make([]int, DefaultShards)

	for i := range n {
		shard := r.GetShard(fmt.Sprintf("group_%d", i))
		counts[shard]++
	}

	expected := float64(n) / float64(DefaultShards) // 97.66
	// Allow ±40% per-bucket deviation. Jump Consistent Hash is theoretically
	// uniform; FNV-64a on sequential strings introduces some non-uniformity,
	// so we use a generous tolerance to verify "relatively uniform" per the AC
	// without making the test flaky.
	tolerance := expected * 0.40

	for shard, count := range counts {
		if math.Abs(float64(count)-expected) > tolerance {
			t.Errorf("shard %d has count %d, expected ~%.0f (±%.0f)", shard, count, expected, tolerance)
		}
	}
}

func TestGetShard_CustomShardCount(t *testing.T) {
	r := New(16)
	for i := range 1000 {
		shard := r.GetShard(fmt.Sprintf("g%d", i))
		if shard < 0 || shard >= 16 {
			t.Errorf("GetShard with 16 shards returned %d", shard)
		}
	}
}

func TestNew_InvalidShardCount(t *testing.T) {
	r := New(0)
	if r.NumShards() != DefaultShards {
		t.Errorf("New(0).NumShards() = %d, want %d", r.NumShards(), DefaultShards)
	}
	r2 := New(-5)
	if r2.NumShards() != DefaultShards {
		t.Errorf("New(-5).NumShards() = %d, want %d", r2.NumShards(), DefaultShards)
	}
}

func BenchmarkGetShard(b *testing.B) {
	r := New(DefaultShards)
	b.ResetTimer()
	for i := range b.N {
		r.GetShard(fmt.Sprintf("group_%d", i))
	}
}
