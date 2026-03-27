package egress

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/obrel/nexus/internal/infra/hash"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newRegistry(t testing.TB, nc *nats.Conn) (*GroupRegistry, *ConnManager) {
	t.Helper()
	mgr := NewConnManager(newMockPresence(), nc, "node-1")
	ring := hash.New(hash.DefaultShards)
	return NewGroupRegistry(mgr, ring, nc), mgr
}

// ── Unit tests (no NATS) ─────────────────────────────────────────────────────

func TestGroupRegistry_Join_TracksMembership(t *testing.T) {
	reg, _ := newRegistry(t, nil)

	reg.Join("app1", "user1", "room_a")
	reg.Join("app1", "user2", "room_a")

	gKey := groupKey("app1", "room_a")
	v, ok := reg.groups.Load(gKey)
	if !ok {
		t.Fatal("expected group entry after Join")
	}
	members := v.(map[string]struct{})
	if _, ok := members["user1"]; !ok {
		t.Error("user1 not in members")
	}
	if _, ok := members["user2"]; !ok {
		t.Error("user2 not in members")
	}
}

func TestGroupRegistry_Leave_RemovesMember(t *testing.T) {
	reg, _ := newRegistry(t, nil)

	reg.Join("app1", "user1", "room_a")
	reg.Join("app1", "user2", "room_a")
	reg.Leave("app1", "user1", "room_a")

	gKey := groupKey("app1", "room_a")
	v, ok := reg.groups.Load(gKey)
	if !ok {
		t.Fatal("expected group to still exist with user2")
	}
	members := v.(map[string]struct{})
	if _, ok := members["user1"]; ok {
		t.Error("user1 should have been removed")
	}
	if _, ok := members["user2"]; !ok {
		t.Error("user2 should still be present")
	}
}

func TestGroupRegistry_Leave_CleansUpEmptyGroup(t *testing.T) {
	reg, _ := newRegistry(t, nil)

	reg.Join("app1", "user1", "room_a")
	reg.Leave("app1", "user1", "room_a")

	gKey := groupKey("app1", "room_a")
	if _, ok := reg.groups.Load(gKey); ok {
		t.Error("expected empty group to be removed from registry")
	}
}

func TestGroupRegistry_LeaveAll_RemovesAllGroups(t *testing.T) {
	reg, _ := newRegistry(t, nil)

	reg.Join("app1", "user1", "room_a")
	reg.Join("app1", "user1", "room_b")
	reg.Join("app1", "user1", "room_c")

	reg.LeaveAll("app1", "user1")

	for _, g := range []string{"room_a", "room_b", "room_c"} {
		if _, ok := reg.groups.Load(groupKey("app1", g)); ok {
			t.Errorf("group %s should be empty after LeaveAll", g)
		}
	}
	if _, ok := reg.userGroups.Load(connKey("app1", "user1")); ok {
		t.Error("user index should be removed after LeaveAll")
	}
}

func TestGroupRegistry_LeaveAll_Idempotent(t *testing.T) {
	reg, _ := newRegistry(t, nil)
	// Should not panic when called for a user who never joined.
	reg.LeaveAll("app1", "ghost")
}

func TestGroupRegistry_HandleControl_Join(t *testing.T) {
	reg, _ := newRegistry(t, nil)

	reg.HandleControl("app1", "user1", []byte(`{"_ctrl":"grp_join","group_id":"room_x"}`))

	if _, ok := reg.groups.Load(groupKey("app1", "room_x")); !ok {
		t.Error("expected user1 to be in room_x after control join")
	}
}

func TestGroupRegistry_HandleControl_Leave(t *testing.T) {
	reg, _ := newRegistry(t, nil)

	reg.Join("app1", "user1", "room_x")
	reg.HandleControl("app1", "user1", []byte(`{"_ctrl":"grp_leave","group_id":"room_x"}`))

	if _, ok := reg.groups.Load(groupKey("app1", "room_x")); ok {
		t.Error("expected user1 to be removed from room_x after control leave")
	}
}

func TestGroupRegistry_HandleControl_IgnoresMalformed(t *testing.T) {
	reg, _ := newRegistry(t, nil)
	// Must not panic on invalid JSON or missing group_id.
	reg.HandleControl("app1", "user1", []byte(`not json`))
	reg.HandleControl("app1", "user1", []byte(`{"_ctrl":"grp_join"}`))
}

// ── NATS integration tests ────────────────────────────────────────────────────

func TestGroupRegistry_Join_SubscribesShardWildcard(t *testing.T) {
	nc := startTestNATS(t)
	reg, _ := newRegistry(t, nc)

	reg.Join("app1", "user1", "room_general")

	ring := hash.New(hash.DefaultShards)
	shard := ring.GetShard("room_general")
	subKey := shardSubKey("app1", shard)

	v, ok := reg.shardSubs.Load(subKey)
	if !ok {
		t.Fatal("expected shard subscription after Join")
	}
	sub := v.(*nats.Subscription)
	if !sub.IsValid() {
		t.Error("shard subscription should be valid")
	}
	expectedSubject := fmt.Sprintf("nexus.app1.v1.grp.%d.>", shard)
	if sub.Subject != expectedSubject {
		t.Errorf("subject = %q, want %q", sub.Subject, expectedSubject)
	}
}

func TestGroupRegistry_Leave_UnsubscribesWhenShardEmpty(t *testing.T) {
	nc := startTestNATS(t)
	reg, _ := newRegistry(t, nc)

	reg.Join("app1", "user1", "room_general")

	ring := hash.New(hash.DefaultShards)
	shard := ring.GetShard("room_general")
	subKey := shardSubKey("app1", shard)

	// Capture the sub before leave.
	v, _ := reg.shardSubs.Load(subKey)
	sub := v.(*nats.Subscription)

	reg.Leave("app1", "user1", "room_general")

	if _, ok := reg.shardSubs.Load(subKey); ok {
		t.Error("expected shard subscription to be removed when no local members remain")
	}
	if sub.IsValid() {
		t.Error("expected shard subscription to be invalidated")
	}
}

func TestGroupRegistry_TwoGroupsSameShard_ShareOneSubscription(t *testing.T) {
	nc := startTestNATS(t)
	reg, _ := newRegistry(t, nc)

	ring := hash.New(hash.DefaultShards)

	// Find two groups that hash to the same shard.
	var groupA, groupB string
	targetShard := ring.GetShard("room_general")
	for i := 0; i < 10_000; i++ {
		g := fmt.Sprintf("group_%d", i)
		if ring.GetShard(g) == targetShard {
			if groupA == "" {
				groupA = g
			} else {
				groupB = g
				break
			}
		}
	}
	if groupA == "" || groupB == "" {
		t.Skip("could not find two groups with the same shard in test range")
	}

	reg.Join("app1", "user1", groupA)
	reg.Join("app1", "user2", groupB)

	subKey := shardSubKey("app1", targetShard)
	count := 0
	reg.shardSubs.Range(func(k, _ any) bool {
		if k.(string) == subKey {
			count++
		}
		return true
	})
	if count != 1 {
		t.Errorf("expected exactly 1 shard subscription, got %d", count)
	}
}

func TestGroupRegistry_Leave_KeepsSubWhenOtherGroupInShard(t *testing.T) {
	nc := startTestNATS(t)
	reg, _ := newRegistry(t, nc)

	ring := hash.New(hash.DefaultShards)
	targetShard := ring.GetShard("room_general")

	var groupB string
	for i := 0; i < 10_000; i++ {
		g := fmt.Sprintf("group_%d", i)
		if ring.GetShard(g) == targetShard {
			groupB = g
			break
		}
	}
	if groupB == "" {
		t.Skip("could not find a colliding group")
	}

	reg.Join("app1", "user1", "room_general")
	reg.Join("app1", "user2", groupB)

	// Leaving room_general should NOT unsubscribe — groupB still has a local member.
	reg.Leave("app1", "user1", "room_general")

	subKey := shardSubKey("app1", targetShard)
	if _, ok := reg.shardSubs.Load(subKey); !ok {
		t.Error("shard subscription should remain while groupB has a local member")
	}
}

// TestGroupRegistry_NATSDeliver_PushesToGroupMembers is the core T-09 test:
// a message published to nexus.{appID}.v1.grp.{shard}.{groupID} must be
// delivered to every locally connected member of that group.
func TestGroupRegistry_NATSDeliver_PushesToGroupMembers(t *testing.T) {
	nc := startTestNATS(t)
	reg, mgr := newRegistry(t, nc)

	srv1, cli1 := dialTestServer(t)
	srv2, cli2 := dialTestServer(t)
	defer srv1.Close()
	defer srv2.Close()
	defer cli1.Close()
	defer cli2.Close()

	ctx := context.Background()
	_ = mgr.Connect(ctx, srv1, "app1", "user1")
	_ = mgr.Connect(ctx, srv2, "app1", "user2")

	reg.Join("app1", "user1", "room_general")
	reg.Join("app1", "user2", "room_general")

	ring := hash.New(hash.DefaultShards)
	shard := ring.GetShard("room_general")
	subject := fmt.Sprintf("nexus.app1.v1.grp.%d.room_general", shard)

	payload := []byte(`{"text":"hello group"}`)
	_ = nc.Publish(subject, payload)
	nc.Flush() //nolint:errcheck

	deadline := time.Now().Add(time.Second)
	for _, cli := range []*websocket.Conn{cli1, cli2} {
		cli.SetReadDeadline(deadline) //nolint:errcheck
		_, got, err := cli.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		if string(got) != string(payload) {
			t.Errorf("payload = %q, want %q", got, payload)
		}
	}
}

func TestGroupRegistry_NATSDeliver_OnlyToGroupMembers(t *testing.T) {
	nc := startTestNATS(t)
	reg, mgr := newRegistry(t, nc)

	srvA, cliA := dialTestServer(t)
	srvB, cliB := dialTestServer(t)
	defer srvA.Close()
	defer srvB.Close()
	defer cliA.Close()
	defer cliB.Close()

	ctx := context.Background()
	_ = mgr.Connect(ctx, srvA, "app1", "userA")
	_ = mgr.Connect(ctx, srvB, "app1", "userB")

	// Only userA joins room_general; userB does not.
	reg.Join("app1", "userA", "room_general")

	ring := hash.New(hash.DefaultShards)
	shard := ring.GetShard("room_general")
	subject := fmt.Sprintf("nexus.app1.v1.grp.%d.room_general", shard)

	_ = nc.Publish(subject, []byte(`{"text":"only A"}`))
	nc.Flush() //nolint:errcheck

	// userA receives it.
	cliA.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	_, got, err := cliA.ReadMessage()
	if err != nil {
		t.Fatalf("userA ReadMessage: %v", err)
	}
	if string(got) != `{"text":"only A"}` {
		t.Errorf("userA payload = %q", got)
	}

	// userB must NOT receive it.
	cliB.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) //nolint:errcheck
	_, _, err = cliB.ReadMessage()
	if err == nil {
		t.Error("userB should not receive a message they did not subscribe to")
	}
}

func TestGroupRegistry_NATSDeliver_AfterLeaveAll(t *testing.T) {
	nc := startTestNATS(t)
	reg, mgr := newRegistry(t, nc)

	srv, cli := dialTestServer(t)
	defer cli.Close()

	ctx := context.Background()
	_ = mgr.Connect(ctx, srv, "app1", "user1")
	reg.Join("app1", "user1", "room_general")

	// Simulate disconnect cleanup.
	reg.LeaveAll("app1", "user1")
	mgr.Disconnect(ctx, "app1", "user1")

	ring := hash.New(hash.DefaultShards)
	shard := ring.GetShard("room_general")
	subject := fmt.Sprintf("nexus.app1.v1.grp.%d.room_general", shard)
	_ = nc.Publish(subject, []byte(`{"text":"ghost"}`))
	nc.Flush() //nolint:errcheck

	// No delivery expected.
	cli.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) //nolint:errcheck
	_, _, err := cli.ReadMessage()
	if err == nil {
		t.Error("expected no delivery after LeaveAll")
	}
}

// TestJoin_ConcurrentJoins verifies that simultaneous joins from many goroutines
// result in correct membership (no lost writes, no races).
func TestJoin_ConcurrentJoins(t *testing.T) {
	reg, _ := newRegistry(t, nil)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			reg.Join("app1", fmt.Sprintf("user%d", i), "room_concurrent")
		}(i)
	}
	wg.Wait()

	gKey := groupKey("app1", "room_concurrent")
	v, ok := reg.groups.Load(gKey)
	if !ok {
		t.Fatal("expected group entry after concurrent joins")
	}
	members := v.(map[string]struct{})
	if len(members) != goroutines {
		t.Errorf("members = %d, want %d", len(members), goroutines)
	}
}

// TestFilterAndPush_SkipsDisconnected verifies that a user in the membership map
// but no longer in the ConnManager (e.g., disconnected between snapshot and write)
// is skipped without error.
func TestFilterAndPush_SkipsDisconnected(t *testing.T) {
	reg, mgr := newRegistry(t, nil)

	// Register user in group but do NOT connect them in the ConnManager.
	reg.Join("app1", "ghost", "room_general")

	// filterAndPush should not panic or error when mgr.Get returns false.
	reg.filterAndPush("app1", "room_general", []byte(`{"text":"no one home"}`))

	// Connect a second user so the group has a real member to verify delivery still works.
	srv, cli := dialTestServer(t)
	defer srv.Close()
	defer cli.Close()
	ctx := context.Background()
	_ = mgr.Connect(ctx, srv, "app1", "real")
	reg.Join("app1", "real", "room_general")

	reg.filterAndPush("app1", "room_general", []byte(`{"text":"hi real"}`))

	cli.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	_, got, err := cli.ReadMessage()
	if err != nil {
		t.Fatalf("real user ReadMessage: %v", err)
	}
	if string(got) != `{"text":"hi real"}` {
		t.Errorf("payload = %q", got)
	}
}

// TestJoin_IdempotentDoesNotDoubleCount verifies that joining the same group twice
// does not double the shard member count (which would prevent unsubscription).
func TestJoin_IdempotentDoesNotDoubleCount(t *testing.T) {
	nc := startTestNATS(t)
	reg, _ := newRegistry(t, nc)

	ring := hash.New(hash.DefaultShards)
	shard := ring.GetShard("room_general")

	reg.Join("app1", "user1", "room_general")
	reg.Join("app1", "user1", "room_general") // duplicate

	count := reg.getShardCount("app1", shard).Load()
	if count != 1 {
		t.Errorf("shard count = %d after duplicate join, want 1", count)
	}

	// Leaving once should unsubscribe (count drops to 0).
	reg.Leave("app1", "user1", "room_general")

	subKey := shardSubKey("app1", shard)
	if _, ok := reg.shardSubs.Load(subKey); ok {
		t.Error("expected shard subscription to be removed after leave")
	}
}

// BenchmarkGroupRegistry_FilterAndPush_1000Members measures delivery overhead
// for a large group (1000 local members).
func BenchmarkGroupRegistry_FilterAndPush_1000Members(b *testing.B) {
	reg, mgr := newRegistry(b, nil)
	ctx := context.Background()

	const n = 1000
	for i := range n {
		srv, _ := dialTestServer(b)
		userID := fmt.Sprintf("user%d", i)
		_ = mgr.Connect(ctx, srv, "app1", userID)
		reg.Join("app1", userID, "bench_room")
	}

	payload := []byte(`{"text":"bench"}`)
	b.ResetTimer()
	for range b.N {
		reg.filterAndPush("app1", "bench_room", payload)
	}
}

// BenchmarkConcurrentJoinLeave measures throughput under concurrent join/leave churn.
func BenchmarkConcurrentJoinLeave(b *testing.B) {
	reg, _ := newRegistry(b, nil)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			userID := fmt.Sprintf("user%d", i%100)
			groupID := fmt.Sprintf("room%d", i%10)
			reg.Join("app1", userID, groupID)
			reg.Leave("app1", userID, groupID)
			i++
		}
	})
}

// BenchmarkGroupRegistry_FilterAndPush_10Members measures delivery to a small group.
func BenchmarkGroupRegistry_FilterAndPush_10Members(b *testing.B) {
	nc := startTestNATS(b)
	reg, mgr := newRegistry(b, nc)
	ctx := context.Background()

	const n = 10
	clients := make([]*websocket.Conn, n)
	for i := range n {
		srv, cli := dialTestServer(b)
		userID := fmt.Sprintf("user%d", i)
		_ = mgr.Connect(ctx, srv, "app1", userID)
		reg.Join("app1", userID, "bench_room")
		clients[i] = cli
	}

	ring := hash.New(hash.DefaultShards)
	shard := ring.GetShard("bench_room")
	subject := fmt.Sprintf("nexus.app1.v1.grp.%d.bench_room", shard)
	payload := []byte(`{"text":"bench"}`)

	b.ResetTimer()
	for range b.N {
		_ = nc.Publish(subject, payload)
		nc.Flush() //nolint:errcheck
		for _, cli := range clients {
			cli.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
			_, _, _ = cli.ReadMessage()
		}
	}
}
