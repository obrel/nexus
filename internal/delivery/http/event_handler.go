package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/usecase"
)

// NewEventSendHandler creates an HTTP handler for POST /v1/events/send.
func NewEventSendHandler(uc usecase.EventUseCase) http.HandlerFunc {
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
			GroupID string `json:"group_id"`
			UserID  string `json:"user_id"`
			Content struct {
				Type  string          `json:"type"`
				Value json.RawMessage `json:"value"`
			} `json:"content"`
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
		if req.Content.Type == "" {
			respondError(w, http.StatusBadRequest, "content_type_required")
			return
		}
		// value is optional; when provided it must be a JSON object.
		if len(req.Content.Value) > 0 {
			trimmed := bytes.TrimSpace(req.Content.Value)
			if len(trimmed) == 0 || trimmed[0] != '{' {
				respondError(w, http.StatusBadRequest, "content_value_must_be_object")
				return
			}
			if !json.Valid(req.Content.Value) {
				respondError(w, http.StatusBadRequest, "invalid_content_value_json")
				return
			}
		}

		evt := &domain.CustomEvent{
			RecipientType: rt,
			RecipientID:   recipientID,
			EventType:     req.Content.Type,
			Value:         req.Content.Value,
		}

		if err := uc.Send(ctx, appID, userID, evt); err != nil {
			log.Errorf("Failed to send event: %v", err)
			if errors.Is(err, domain.ErrAccessDenied) {
				respondError(w, http.StatusForbidden, "not_a_member")
				return
			}
			respondError(w, http.StatusInternalServerError, "send_failed")
			return
		}

		respondSuccess(w, http.StatusAccepted, map[string]any{
			"success": true,
			"data":    map[string]any{},
		})
	}
}
