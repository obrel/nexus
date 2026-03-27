package http

import (
	"encoding/json"
	"net/http"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/usecase"
)

// NewProfileGetHandler creates an HTTP handler for GET /v1/profile.
func NewProfileGetHandler(uc usecase.ProfileUseCase) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		appID := domain.AppIDFromContext(ctx)
		userID := domain.UserIDFromContext(ctx)
		if appID == "" || userID == "" {
			respondError(w, http.StatusUnauthorized, "missing_auth_context")
			return
		}

		profile, err := uc.Get(ctx, appID, userID)
		if err != nil {
			log.Errorf("Failed to get profile: %v", err)
			respondError(w, http.StatusInternalServerError, "profile_get_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data":    profile,
		})
	}
}

// NewProfileUpdateHandler creates an HTTP handler for POST /v1/profile.
func NewProfileUpdateHandler(uc usecase.ProfileUseCase) http.HandlerFunc {
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
			Name string `json:"name"`
			Bio  string `json:"bio"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}

		profile := &domain.UserProfile{
			Name: req.Name,
			Bio:  req.Bio,
		}

		if err := uc.Update(ctx, appID, userID, profile); err != nil {
			log.Errorf("Failed to update profile: %v", err)
			respondError(w, http.StatusInternalServerError, "profile_update_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data":    map[string]any{},
		})
	}
}
