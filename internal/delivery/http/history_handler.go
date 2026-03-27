package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/usecase"
)

// NewHistoryHandler creates an HTTP handler for GET /v1/history.
func NewHistoryHandler(uc usecase.HistoryUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)
		userID := domain.UserIDFromContext(ctx)
		if appID == "" || userID == "" {
			respondError(w, http.StatusUnauthorized, "missing_auth_context")
			return
		}

		groupID := r.URL.Query().Get("group_id")
		targetUserID := r.URL.Query().Get("user_id")

		// group_id and user_id are mutually exclusive; exactly one must be provided.
		var rt domain.RecipientType
		var recipientID string
		switch {
		case groupID != "" && targetUserID != "":
			respondError(w, http.StatusBadRequest, "group_id_and_user_id_are_mutually_exclusive")
			return
		case groupID != "":
			rt = domain.RecipientGroup
			recipientID = groupID
		case targetUserID != "":
			rt = domain.RecipientUser
			recipientID = targetUserID
		default:
			respondError(w, http.StatusBadRequest, "group_id_or_user_id_required")
			return
		}
		if !isValidID(recipientID) {
			respondError(w, http.StatusBadRequest, "invalid_recipient_id")
			return
		}

		var sinceID uint64
		if s := r.URL.Query().Get("since_id"); s != "" {
			var err error
			sinceID, err = strconv.ParseUint(s, 10, 64)
			if err != nil {
				respondError(w, http.StatusBadRequest, "invalid_since_id")
				return
			}
		}

		limit := 0
		if l := r.URL.Query().Get("limit"); l != "" {
			var err error
			limit, err = strconv.Atoi(l)
			if err != nil {
				respondError(w, http.StatusBadRequest, "invalid_limit")
				return
			}
		}

		msgs, err := uc.GetHistory(ctx, appID, userID, rt, recipientID, sinceID, limit)
		if err != nil {
			if errors.Is(err, domain.ErrAccessDenied) {
				respondError(w, http.StatusForbidden, "not_a_group_member")
				return
			}
			log.Errorf("Failed to get history: %v", err)
			respondError(w, http.StatusInternalServerError, "history_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data": map[string]any{
				"messages": msgs,
			},
		})
	}
}
