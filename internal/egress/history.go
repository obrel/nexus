package egress

import (
	"context"
	"encoding/json"

	"github.com/gorilla/websocket"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// catchUpLimit caps the number of messages returned per catch-up request to prevent
// overwhelming a reconnecting client with a large backlog.
const catchUpLimit = 25

// HistoryService fetches missed messages from MySQL and pushes them to a
// WebSocket connection. It is used during reconnection (catch-up phase) to
// fill the gap between the client's last known message and the current stream.
type HistoryService struct {
	repo domain.MessageRepository
	mgr  *ConnManager
	log  logger.Logger
}

// NewHistoryService creates a HistoryService backed by repo.
// mgr is used to obtain the per-connection write mutex for safe concurrent writes.
func NewHistoryService(repo domain.MessageRepository, mgr *ConnManager) *HistoryService {
	return &HistoryService{
		repo: repo,
		mgr:  mgr,
		log:  logger.For("egress", "history"),
	}
}

// CatchUp queries the messages table for all messages in groupID with
// id > lastMsgID (up to catchUpLimit) and writes them to conn in order.
// Writes are serialized via the per-connection write mutex so they do not
// interleave with concurrent real-time message delivery.
// If lastMsgID is 0, no query is performed.
func (h *HistoryService) CatchUp(ctx context.Context, conn *websocket.Conn, appID, userID, groupID string, lastMsgID uint64) {
	if lastMsgID == 0 {
		return
	}

	msgs, err := h.repo.GetGroupMessages(ctx, appID, groupID, lastMsgID, catchUpLimit)
	if err != nil {
		h.log.Warnf("history: catch-up %s/%s since %d: %v", appID, groupID, lastMsgID, err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	wmu := h.mgr.getWriteMu(connKey(appID, userID))
	for _, msg := range msgs {
		data, err := json.Marshal(msg)
		if err != nil {
			h.log.Warnf("history: marshal msg %d: %v", msg.ID, err)
			continue
		}
		wmu.Lock()
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			h.log.Warnf("history: write to %s/%s: %v", appID, userID, err)
			wmu.Unlock()
			return // connection closed; stop early
		}
		wmu.Unlock()
	}

	h.log.Infof("history: pushed %d catch-up messages to %s/%s for group %s (since %d)",
		len(msgs), appID, userID, groupID, lastMsgID)
}
