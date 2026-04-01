package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/usecase"
)

// NewInternalGroupCreateHandler creates an HTTP handler for POST /internal/v1/groups/create.
func NewInternalGroupCreateHandler(uc usecase.GroupUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		var req struct {
			UserID      string `json:"user_id"`
			GroupID     string `json:"group_id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.UserID == "" {
			respondError(w, http.StatusBadRequest, "user_id_required")
			return
		}
		if req.GroupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(req.GroupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}
		if req.Name == "" {
			respondError(w, http.StatusBadRequest, "name_required")
			return
		}

		if err := uc.Create(ctx, appID, req.UserID, req.GroupID, req.Name, req.Description); err != nil {
			log.Errorf("Failed to create group (internal): %v", err)
			if errors.Is(err, domain.ErrAlreadyExists) {
				respondError(w, http.StatusConflict, "group_already_exists")
				return
			}
			respondError(w, http.StatusInternalServerError, "create_failed")
			return
		}

		respondSuccess(w, http.StatusCreated, map[string]any{
			"success": true,
			"data":    map[string]string{"group_id": req.GroupID},
		})
	}
}

// NewInternalGroupGetHandler creates an HTTP handler for GET /internal/v1/groups/detail.
func NewInternalGroupGetHandler(uc usecase.GroupUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		userID := r.URL.Query().Get("user_id")
		groupID := r.URL.Query().Get("group_id")

		if userID == "" {
			respondError(w, http.StatusBadRequest, "user_id_required")
			return
		}
		if groupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(groupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}

		group, err := uc.Get(ctx, appID, userID, groupID)
		if err != nil {
			log.Errorf("Failed to get group (internal): %v", err)
			if errors.Is(err, domain.ErrNotFound) {
				respondError(w, http.StatusNotFound, "group_not_found")
				return
			}
			if errors.Is(err, domain.ErrAccessDenied) {
				respondError(w, http.StatusForbidden, "not_a_member")
				return
			}
			respondError(w, http.StatusInternalServerError, "get_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data": map[string]any{
				"group_id":    group.ID,
				"name":        group.Name,
				"description": group.Description,
				"created_by":  group.CreatedBy,
				"created_at":  group.CreatedAt,
			},
		})
	}
}

// NewInternalGroupUpdateHandler creates an HTTP handler for POST /internal/v1/groups/update.
func NewInternalGroupUpdateHandler(uc usecase.GroupUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		var req struct {
			UserID      string `json:"user_id"`
			GroupID     string `json:"group_id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.UserID == "" {
			respondError(w, http.StatusBadRequest, "user_id_required")
			return
		}
		if req.GroupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(req.GroupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}

		if err := uc.Update(ctx, appID, req.UserID, req.GroupID, req.Name, req.Description); err != nil {
			log.Errorf("Failed to update group (internal): %v", err)
			if errors.Is(err, domain.ErrNotFound) {
				respondError(w, http.StatusNotFound, "group_not_found")
				return
			}
			if errors.Is(err, domain.ErrAccessDenied) {
				respondError(w, http.StatusForbidden, "not_a_member")
				return
			}
			respondError(w, http.StatusInternalServerError, "update_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data":    map[string]string{"group_id": req.GroupID},
		})
	}
}

// NewInternalGroupJoinHandler creates an HTTP handler for POST /internal/v1/groups/join.
func NewInternalGroupJoinHandler(uc usecase.GroupUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		var req struct {
			UserID  string `json:"user_id"`
			GroupID string `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.UserID == "" {
			respondError(w, http.StatusBadRequest, "user_id_required")
			return
		}
		if req.GroupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(req.GroupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}

		if err := uc.Join(ctx, appID, req.UserID, req.GroupID); err != nil {
			log.Errorf("Failed to join group (internal): %v", err)
			if errors.Is(err, domain.ErrNotFound) {
				respondError(w, http.StatusNotFound, "group_not_found")
				return
			}
			respondError(w, http.StatusInternalServerError, "join_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data":    map[string]string{"group_id": req.GroupID},
		})
	}
}

// NewInternalGroupLeaveHandler creates an HTTP handler for POST /internal/v1/groups/leave.
func NewInternalGroupLeaveHandler(uc usecase.GroupUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)

		var req struct {
			UserID  string `json:"user_id"`
			GroupID string `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.UserID == "" {
			respondError(w, http.StatusBadRequest, "user_id_required")
			return
		}
		if req.GroupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(req.GroupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}

		if err := uc.Leave(ctx, appID, req.UserID, req.GroupID); err != nil {
			log.Errorf("Failed to leave group (internal): %v", err)
			respondError(w, http.StatusInternalServerError, "leave_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data":    map[string]string{"group_id": req.GroupID},
		})
	}
}

// NewGroupCreateHandler creates an HTTP handler for POST /v1/groups/create.
func NewGroupCreateHandler(uc usecase.GroupUseCase) http.HandlerFunc {
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
			GroupID     string `json:"group_id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.GroupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(req.GroupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}
		if req.Name == "" {
			respondError(w, http.StatusBadRequest, "name_required")
			return
		}

		if err := uc.Create(ctx, appID, userID, req.GroupID, req.Name, req.Description); err != nil {
			log.Errorf("Failed to create group: %v", err)
			if errors.Is(err, domain.ErrAlreadyExists) {
				respondError(w, http.StatusConflict, "group_already_exists")
				return
			}
			respondError(w, http.StatusInternalServerError, "create_failed")
			return
		}

		respondSuccess(w, http.StatusCreated, map[string]any{
			"success": true,
			"data": map[string]string{
				"group_id": req.GroupID,
			},
		})
	}
}

// NewGroupGetHandler creates an HTTP handler for GET /v1/groups/detail.
func NewGroupGetHandler(uc usecase.GroupUseCase) http.HandlerFunc {
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
		if groupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(groupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}

		group, err := uc.Get(ctx, appID, userID, groupID)
		if err != nil {
			log.Errorf("Failed to get group: %v", err)
			if errors.Is(err, domain.ErrNotFound) {
				respondError(w, http.StatusNotFound, "group_not_found")
				return
			}
			if errors.Is(err, domain.ErrAccessDenied) {
				respondError(w, http.StatusForbidden, "not_a_member")
				return
			}
			respondError(w, http.StatusInternalServerError, "get_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data": map[string]any{
				"group_id":    group.ID,
				"name":        group.Name,
				"description": group.Description,
				"created_by":  group.CreatedBy,
				"created_at":  group.CreatedAt,
			},
		})
	}
}

// NewGroupUpdateHandler creates an HTTP handler for POST /v1/groups/update.
func NewGroupUpdateHandler(uc usecase.GroupUseCase) http.HandlerFunc {
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
			GroupID     string `json:"group_id"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.GroupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(req.GroupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}

		if err := uc.Update(ctx, appID, userID, req.GroupID, req.Name, req.Description); err != nil {
			log.Errorf("Failed to update group: %v", err)
			if errors.Is(err, domain.ErrNotFound) {
				respondError(w, http.StatusNotFound, "group_not_found")
				return
			}
			if errors.Is(err, domain.ErrAccessDenied) {
				respondError(w, http.StatusForbidden, "not_a_member")
				return
			}
			respondError(w, http.StatusInternalServerError, "update_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data": map[string]string{
				"group_id": req.GroupID,
			},
		})
	}
}

// NewGroupJoinHandler creates an HTTP handler for POST /v1/groups/join.
func NewGroupJoinHandler(uc usecase.GroupUseCase) http.HandlerFunc {
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
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.GroupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(req.GroupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}

		if err := uc.Join(ctx, appID, userID, req.GroupID); err != nil {
			log.Errorf("Failed to join group: %v", err)
			if errors.Is(err, domain.ErrNotFound) {
				respondError(w, http.StatusNotFound, "group_not_found")
				return
			}
			respondError(w, http.StatusInternalServerError, "join_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data": map[string]string{
				"group_id": req.GroupID,
			},
		})
	}
}

// NewGroupLeaveHandler creates an HTTP handler for POST /v1/groups/leave.
func NewGroupLeaveHandler(uc usecase.GroupUseCase) http.HandlerFunc {
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
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.GroupID == "" {
			respondError(w, http.StatusBadRequest, "group_id_required")
			return
		}
		if !isValidID(req.GroupID) {
			respondError(w, http.StatusBadRequest, "invalid_group_id")
			return
		}

		if err := uc.Leave(ctx, appID, userID, req.GroupID); err != nil {
			log.Errorf("Failed to leave group: %v", err)
			respondError(w, http.StatusInternalServerError, "leave_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data": map[string]string{
				"group_id": req.GroupID,
			},
		})
	}
}
