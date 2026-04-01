package http

import (
	"encoding/json"
	"net/http"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/usecase"
)

// NewInternalRegisterHandler creates an HTTP handler for POST /internal/v1/auth/register.
// Called by external auth services to sync users into Nexus and obtain a JWT.
func NewInternalRegisterHandler(uc usecase.AuthUseCase, defaultAppID string) http.HandlerFunc {
	log := logger.For("delivery", "http")
	return func(w http.ResponseWriter, r *http.Request) {
		appID := domain.AppIDFromContext(r.Context())
		if appID == "" {
			appID = defaultAppID
		}

		var req struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if req.Email == "" {
			respondError(w, http.StatusBadRequest, "email_required")
			return
		}

		token, userID, err := uc.RegisterUser(r.Context(), appID, req.Email, req.Name)
		if err != nil {
			log.Errorf("Failed to register user: %v", err)
			respondError(w, http.StatusInternalServerError, "register_failed")
			return
		}

		respondSuccess(w, http.StatusOK, map[string]any{
			"success": true,
			"data": map[string]any{
				"token":   token,
				"user_id": userID,
			},
		})
	}
}
