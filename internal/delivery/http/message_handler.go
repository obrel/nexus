package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/usecase"
)

// NewMessageSendHandler creates an HTTP handler for POST /v1/messages/send.
func NewMessageSendHandler(uc usecase.MessageUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)
		userID := domain.UserIDFromContext(ctx)
		if appID == "" || userID == "" {
			respondError(w, http.StatusUnauthorized, "missing_auth_context")
			return
		}

		var req struct {
			GroupID  string          `json:"group_id"`
			UserID  string          `json:"user_id"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}

		// group_id and user_id are mutually exclusive; exactly one must be provided.
		var rt domain.RecipientType
		var recipientID string
		switch {
		case req.GroupID != "" && req.UserID != "":
			respondError(w, http.StatusBadRequest, "group_id_and_user_id_are_mutually_exclusive")
			return
		case req.GroupID != "":
			rt = domain.RecipientGroup
			recipientID = req.GroupID
		case req.UserID != "":
			rt = domain.RecipientUser
			recipientID = req.UserID
		default:
			respondError(w, http.StatusBadRequest, "group_id_or_user_id_required")
			return
		}
		if !isValidID(recipientID) {
			respondError(w, http.StatusBadRequest, "invalid_recipient_id")
			return
		}
		if rt == domain.RecipientUser && recipientID == userID {
			respondError(w, http.StatusBadRequest, "cannot_message_self")
			return
		}
		if len(req.Content) == 0 {
			respondError(w, http.StatusBadRequest, "content_required")
			return
		}
		if !json.Valid(req.Content) {
			respondError(w, http.StatusBadRequest, "invalid_content_json")
			return
		}

		msg := &domain.Message{
			AppID:         appID,
			RecipientType: rt,
			RecipientID:   recipientID,
			Content:       req.Content,
			SenderType:    domain.SenderUser,
			SenderID:      userID,
		}

		if err := uc.Send(ctx, appID, msg); err != nil {
			log.Errorf("Failed to send message: %v", err)
			if errors.Is(err, domain.ErrAccessDenied) {
				respondError(w, http.StatusForbidden, "not_a_member")
				return
			}
			respondError(w, http.StatusInternalServerError, "send_failed")
			return
		}

		resp := map[string]any{
			"success": true,
			"data": map[string]string{
				"message_id": fmt.Sprintf("%d", msg.ID),
			},
		}
		respondSuccess(w, http.StatusAccepted, resp)
	}
}

// NewInternalMessageSendHandler creates an HTTP handler for POST /internal/v1/messages/send.
// It is intended for service-to-service use; app_id is supplied via X-App-ID header and
// sender_id is supplied in the request body instead of relying on JWT claims.
func NewInternalMessageSendHandler(uc usecase.MessageUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		var req struct {
			SenderID string          `json:"sender_id"`
			GroupID  string          `json:"group_id"`
			UserID   string          `json:"user_id"`
			Content  json.RawMessage `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.SenderID == "" {
			respondError(w, http.StatusBadRequest, "sender_id_required")
			return
		}

		var rt domain.RecipientType
		var recipientID string
		switch {
		case req.GroupID != "" && req.UserID != "":
			respondError(w, http.StatusBadRequest, "group_id_and_user_id_are_mutually_exclusive")
			return
		case req.GroupID != "":
			rt = domain.RecipientGroup
			recipientID = req.GroupID
		case req.UserID != "":
			rt = domain.RecipientUser
			recipientID = req.UserID
		default:
			respondError(w, http.StatusBadRequest, "group_id_or_user_id_required")
			return
		}
		if !isValidID(recipientID) {
			respondError(w, http.StatusBadRequest, "invalid_recipient_id")
			return
		}
		if rt == domain.RecipientUser && recipientID == req.SenderID {
			respondError(w, http.StatusBadRequest, "cannot_message_self")
			return
		}
		if len(req.Content) == 0 {
			respondError(w, http.StatusBadRequest, "content_required")
			return
		}
		if !json.Valid(req.Content) {
			respondError(w, http.StatusBadRequest, "invalid_content_json")
			return
		}

		msg := &domain.Message{
			AppID:         appID,
			RecipientType: rt,
			RecipientID:   recipientID,
			Content:       req.Content,
			SenderType:    domain.SenderUser,
			SenderID:      req.SenderID,
		}

		if err := uc.Send(ctx, appID, msg); err != nil {
			log.Errorf("Failed to send internal message: %v", err)
			if errors.Is(err, domain.ErrAccessDenied) {
				respondError(w, http.StatusForbidden, "not_a_member")
				return
			}
			respondError(w, http.StatusInternalServerError, "send_failed")
			return
		}

		resp := map[string]any{
			"success": true,
			"data": map[string]string{
				"message_id": fmt.Sprintf("%d", msg.ID),
			},
		}
		respondSuccess(w, http.StatusAccepted, resp)
	}
}

// NewInternalMessageEditHandler creates an HTTP handler for POST /internal/v1/messages/edit.
func NewInternalMessageEditHandler(uc usecase.MessageUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		var req struct {
			SenderID  string          `json:"sender_id"`
			MessageID string          `json:"message_id"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.SenderID == "" {
			respondError(w, http.StatusBadRequest, "sender_id_required")
			return
		}
		if req.MessageID == "" {
			respondError(w, http.StatusBadRequest, "message_id_required")
			return
		}
		if len(req.Content) == 0 {
			respondError(w, http.StatusBadRequest, "content_required")
			return
		}

		msgID, err := strconv.ParseUint(req.MessageID, 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid_message_id")
			return
		}
		if !json.Valid(req.Content) {
			respondError(w, http.StatusBadRequest, "invalid_content_json")
			return
		}

		if err := uc.Edit(ctx, appID, req.SenderID, msgID, string(req.Content)); err != nil {
			log.Errorf("Failed to edit message (internal): %v", err)
			respondError(w, http.StatusInternalServerError, "edit_failed")
			return
		}

		resp := map[string]any{
			"success": true,
			"data":    map[string]string{"message_id": req.MessageID},
		}
		respondSuccess(w, http.StatusOK, resp)
	}
}

// NewInternalMessageDeleteHandler creates an HTTP handler for POST /internal/v1/messages/delete.
func NewInternalMessageDeleteHandler(uc usecase.MessageUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		var req struct {
			SenderID  string `json:"sender_id"`
			MessageID string `json:"message_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.SenderID == "" {
			respondError(w, http.StatusBadRequest, "sender_id_required")
			return
		}
		if req.MessageID == "" {
			respondError(w, http.StatusBadRequest, "message_id_required")
			return
		}

		msgID, err := strconv.ParseUint(req.MessageID, 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid_message_id")
			return
		}

		if err := uc.Delete(ctx, appID, req.SenderID, msgID); err != nil {
			log.Errorf("Failed to delete message (internal): %v", err)
			respondError(w, http.StatusInternalServerError, "delete_failed")
			return
		}

		resp := map[string]any{
			"success": true,
			"data":    map[string]string{"message_id": req.MessageID},
		}
		respondSuccess(w, http.StatusOK, resp)
	}
}

// NewInternalMessageAckHandler creates an HTTP handler for POST /internal/v1/messages/ack.
func NewInternalMessageAckHandler(uc usecase.MessageUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		var req struct {
			UserID    string `json:"user_id"`
			MessageID string `json:"message_id"`
			Status    string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.UserID == "" {
			respondError(w, http.StatusBadRequest, "user_id_required")
			return
		}
		if req.MessageID == "" {
			respondError(w, http.StatusBadRequest, "message_id_required")
			return
		}
		if req.Status == "" {
			respondError(w, http.StatusBadRequest, "status_required")
			return
		}

		msgID, err := strconv.ParseUint(req.MessageID, 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid_message_id")
			return
		}

		status := domain.MessageStatus(req.Status)
		if status != domain.StatusReceived && status != domain.StatusRead {
			respondError(w, http.StatusBadRequest, "invalid_status")
			return
		}

		if err := uc.Ack(ctx, appID, req.UserID, msgID, status); err != nil {
			log.Errorf("Failed to ack message (internal): %v", err)
			respondError(w, http.StatusInternalServerError, "ack_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{}})
	}
}

// NewMessageEditHandler creates an HTTP handler for POST /v1/messages/edit.
func NewMessageEditHandler(uc usecase.MessageUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)
		userID := domain.UserIDFromContext(ctx)
		if appID == "" || userID == "" {
			respondError(w, http.StatusUnauthorized, "missing_auth_context")
			return
		}

		var req struct {
			MessageID string          `json:"message_id"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.MessageID == "" {
			respondError(w, http.StatusBadRequest, "message_id_required")
			return
		}
		if len(req.Content) == 0 {
			respondError(w, http.StatusBadRequest, "content_required")
			return
		}

		msgID, err := strconv.ParseUint(req.MessageID, 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid_message_id")
			return
		}

		if !json.Valid(req.Content) {
			respondError(w, http.StatusBadRequest, "invalid_content_json")
			return
		}

		if err := uc.Edit(ctx, appID, userID, msgID, string(req.Content)); err != nil {
			log.Errorf("Failed to edit message: %v", err)
			respondError(w, http.StatusInternalServerError, "edit_failed")
			return
		}

		resp := map[string]any{
			"success": true,
			"data": map[string]string{
				"message_id": req.MessageID,
			},
		}
		respondSuccess(w, http.StatusOK, resp)
	}
}

// NewMessageDeleteHandler creates an HTTP handler for POST /v1/messages/delete.
func NewMessageDeleteHandler(uc usecase.MessageUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)
		userID := domain.UserIDFromContext(ctx)
		if appID == "" || userID == "" {
			respondError(w, http.StatusUnauthorized, "missing_auth_context")
			return
		}

		var req struct {
			MessageID string `json:"message_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.MessageID == "" {
			respondError(w, http.StatusBadRequest, "message_id_required")
			return
		}

		msgID, err := strconv.ParseUint(req.MessageID, 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid_message_id")
			return
		}

		if err := uc.Delete(ctx, appID, userID, msgID); err != nil {
			log.Errorf("Failed to delete message: %v", err)
			respondError(w, http.StatusInternalServerError, "delete_failed")
			return
		}

		resp := map[string]any{
			"success": true,
			"data": map[string]string{
				"message_id": req.MessageID,
			},
		}
		respondSuccess(w, http.StatusOK, resp)
	}
}

// NewMessageAckHandler creates an HTTP handler for POST /v1/messages/ack.
func NewMessageAckHandler(uc usecase.MessageUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)
		userID := domain.UserIDFromContext(ctx)
		if appID == "" || userID == "" {
			respondError(w, http.StatusUnauthorized, "missing_auth_context")
			return
		}

		var req struct {
			MessageID string `json:"message_id"`
			Status    string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.MessageID == "" {
			respondError(w, http.StatusBadRequest, "message_id_required")
			return
		}
		if req.Status == "" {
			respondError(w, http.StatusBadRequest, "status_required")
			return
		}

		msgID, err := strconv.ParseUint(req.MessageID, 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid_message_id")
			return
		}

		status := domain.MessageStatus(req.Status)
		if status != domain.StatusReceived && status != domain.StatusRead {
			respondError(w, http.StatusBadRequest, "invalid_status")
			return
		}

		if err := uc.Ack(ctx, appID, userID, msgID, status); err != nil {
			log.Errorf("Failed to ack message: %v", err)
			respondError(w, http.StatusInternalServerError, "ack_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{}})
	}
}
