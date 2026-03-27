package egress

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/obrel/nexus/internal/infra"
	"github.com/obrel/nexus/internal/infra/hash"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/metrics"
)

// GroupRegistry tracks group membership for locally connected users and manages
// shard-level NATS wildcard subscriptions.
//
// Subject pattern: nexus.{appID}.v1.grp.{shard}.{groupID}
// One subscription per shard covers all groups that hash to that shard,
// bounding subscription count to numShards rather than numGroups.
type GroupRegistry struct {
	// "appID:groupID" → map[userID]struct{} — local members per group
	groups sync.Map
	// per-group mutex protecting the inner map[userID]struct{}
	groupMus sync.Map // "appID:groupID" → *sync.Mutex
	// "appID:userID" → *sync.Map{ groupID → struct{} } — reverse index for disconnect cleanup
	userGroups sync.Map
	// "appID:shard" → *nats.Subscription — one wildcard sub per shard
	shardSubs sync.Map
	// "appID:shard" → *atomic.Int32 — total local members across all groups in that shard.
	// Eliminates the O(groups) scan in Leave: unsubscribe exactly when count reaches 0.
	shardCounts sync.Map

	mgr  *ConnManager
	ring *hash.Ring
	nc   *nats.Conn // nil in tests without NATS
	log  logger.Logger

	subPendingMsgsLimit  int // NATS subscription pending messages limit
	subPendingBytesLimit int // NATS subscription pending bytes limit
}

// NewGroupRegistry creates a GroupRegistry.
// nc may be nil — NATS subscriptions are skipped (useful in tests).
func NewGroupRegistry(mgr *ConnManager, ring *hash.Ring, nc *nats.Conn) *GroupRegistry {
	return &GroupRegistry{
		mgr:  mgr,
		ring: ring,
		nc:   nc,
		log:  logger.For("egress", "group_registry"),
	}
}

// SetSubPendingLimits configures NATS subscription pending message and byte limits.
func (r *GroupRegistry) SetSubPendingLimits(msgs, bytes int) {
	r.subPendingMsgsLimit = msgs
	r.subPendingBytesLimit = bytes
}

// Join adds userID to groupID on this node and subscribes to the shard wildcard
// if this is the first local member in that shard.
func (r *GroupRegistry) Join(appID, userID, groupID string) {
	gKey := groupKey(appID, groupID)
	shard := r.ring.GetShard(groupID)

	mu := r.getGroupMu(gKey)
	mu.Lock()
	v, ok := r.groups.Load(gKey)
	var added bool
	if !ok {
		r.groups.Store(gKey, map[string]struct{}{userID: {}})
		added = true
	} else {
		members := v.(map[string]struct{})
		if _, alreadyIn := members[userID]; !alreadyIn {
			members[userID] = struct{}{}
			added = true
		}
	}
	mu.Unlock()

	if !added {
		return // idempotent: user was already in the group
	}

	// Reverse index: appID:userID → {groupID}
	uKey := connKey(appID, userID)
	actual, _ := r.userGroups.LoadOrStore(uKey, &sync.Map{})
	actual.(*sync.Map).Store(groupID, struct{}{})

	// Increment per-shard counter and subscribe wildcard on the first local member.
	count := r.getShardCount(appID, shard).Add(1)
	if r.nc != nil && count == 1 {
		subKey := shardSubKey(appID, shard)
		if _, loaded := r.shardSubs.Load(subKey); !loaded {
			sub, err := r.subscribeShardWildcard(appID, shard)
			if err != nil {
				r.log.Warnf("group registry: subscribe shard %d app %s: %v", shard, appID, err)
				// Roll back the counter increment since we failed to subscribe.
				r.getShardCount(appID, shard).Add(-1)
				return
			}
			// Another goroutine may have subscribed concurrently; drain the loser.
			if _, loaded := r.shardSubs.LoadOrStore(subKey, sub); loaded {
				sub.Unsubscribe() //nolint:errcheck
			}
		}
	}

	r.log.Infof("group registry: %s/%s joined group %s", appID, userID, groupID)
}

// Leave removes userID from groupID. If no local users remain in the shard,
// the shard wildcard subscription is cancelled.
func (r *GroupRegistry) Leave(appID, userID, groupID string) {
	gKey := groupKey(appID, groupID)
	shard := r.ring.GetShard(groupID)

	mu := r.getGroupMu(gKey)
	mu.Lock()
	var removed bool
	if v, ok := r.groups.Load(gKey); ok {
		members := v.(map[string]struct{})
		if _, wasIn := members[userID]; wasIn {
			delete(members, userID)
			removed = true
			if len(members) == 0 {
				r.groups.Delete(gKey)
			}
		}
	}
	mu.Unlock()

	if !removed {
		return // user was not in this group; nothing to clean up
	}

	// Remove from reverse index.
	uKey := connKey(appID, userID)
	if v, ok := r.userGroups.Load(uKey); ok {
		v.(*sync.Map).Delete(groupID)
	}

	// Decrement per-shard counter; unsubscribe when the last local member leaves.
	if r.nc != nil {
		count := r.getShardCount(appID, shard).Add(-1)
		if count <= 0 {
			subKey := shardSubKey(appID, shard)
			if sub, ok := r.shardSubs.LoadAndDelete(subKey); ok {
				sub.(*nats.Subscription).Unsubscribe() //nolint:errcheck
			}
		}
	}

	r.log.Infof("group registry: %s/%s left group %s", appID, userID, groupID)
}

// LeaveAll removes userID from every group they joined. Called on disconnect.
func (r *GroupRegistry) LeaveAll(appID, userID string) {
	uKey := connKey(appID, userID)
	v, ok := r.userGroups.LoadAndDelete(uKey)
	if !ok {
		return
	}
	v.(*sync.Map).Range(func(k, _ any) bool {
		r.Leave(appID, userID, k.(string))
		return true
	})
}

// HandleControl processes inter-tier control messages published by the HTTP Ingress.
// Format: {"_ctrl":"grp_join","group_id":"room_123"} or {"_ctrl":"grp_leave","group_id":"room_123"}
func (r *GroupRegistry) HandleControl(appID, userID string, data []byte) {
	var msg struct {
		Ctrl    string `json:"_ctrl"`
		GroupID string `json:"group_id"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.GroupID == "" {
		return
	}
	switch msg.Ctrl {
	case "grp_join":
		r.Join(appID, userID, msg.GroupID)
	case "grp_leave":
		r.Leave(appID, userID, msg.GroupID)
	}
}

// subscribeShardWildcard subscribes to nexus.{appID}.v1.grp.{shard}.>
// The callback extracts the groupID from the subject and forwards the payload
// to every locally connected member of that group.
func (r *GroupRegistry) subscribeShardWildcard(appID string, shard int) (*nats.Subscription, error) {
	subject := fmt.Sprintf("nexus.%s.v1.grp.%d.>", appID, shard)
	sub, err := r.nc.Subscribe(subject, func(msg *nats.Msg) {
		defer infra.RecoverNATS("shardWildcard:" + subject)
		// Subject: nexus.{appID}.v1.grp.{shard}.{groupID}
		// Parts:     0     1    2   3     4        5
		parts := strings.SplitN(msg.Subject, ".", 6)
		if len(parts) < 6 {
			return
		}
		msgAppID := parts[1]
		groupID := parts[5] // groupID is the final token
		r.filterAndPush(msgAppID, groupID, msg.Data)
	})
	if err != nil {
		return nil, err
	}
	if r.subPendingMsgsLimit > 0 && r.subPendingBytesLimit > 0 {
		sub.SetPendingLimits(r.subPendingMsgsLimit, r.subPendingBytesLimit) //nolint:errcheck
	}
	return sub, nil
}

// filterAndPush delivers data to all locally connected members of groupID.
// The member set is copied under the group mutex so writes happen without holding it.
func (r *GroupRegistry) filterAndPush(appID, groupID string, data []byte) {
	gKey := groupKey(appID, groupID)
	mu := r.getGroupMu(gKey)
	mu.Lock()
	v, ok := r.groups.Load(gKey)
	if !ok {
		mu.Unlock()
		return
	}
	members := make([]string, 0, len(v.(map[string]struct{})))
	for uid := range v.(map[string]struct{}) {
		members = append(members, uid)
	}
	mu.Unlock()

	for _, userID := range members {
		conn, ok := r.mgr.Get(appID, userID)
		if !ok {
			continue // user disconnected between membership snapshot and delivery
		}
		wmu := r.mgr.getWriteMu(connKey(appID, userID))
		wmu.Lock()
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			r.log.Warnf("group registry: deliver to %s/%s group %s: %v", appID, userID, groupID, err)
		} else {
			metrics.MessagesDeliveredTotal.Inc()
		}
		wmu.Unlock()
	}
}

// getShardCount returns (or creates) the atomic member counter for a shard.
func (r *GroupRegistry) getShardCount(appID string, shard int) *atomic.Int32 {
	key := shardSubKey(appID, shard)
	v, _ := r.shardCounts.LoadOrStore(key, &atomic.Int32{})
	return v.(*atomic.Int32)
}

func (r *GroupRegistry) getGroupMu(key string) *sync.Mutex {
	v, _ := r.groupMus.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// groupKey returns the composite key "appID:groupID" used for group membership lookups.
func groupKey(appID, groupID string) string {
	return appID + ":" + groupID
}

// shardSubKey returns the composite key "appID:shard" used for shard subscription tracking.
func shardSubKey(appID string, shard int) string {
	return fmt.Sprintf("%s:%d", appID, shard)
}
