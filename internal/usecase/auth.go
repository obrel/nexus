package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/snowflake"
)

// authUseCase implements AuthUseCase for user registration and JWT issuance.
type authUseCase struct {
	userRepo  domain.UserRepository
	snowflake *snowflake.Generator
	jwtSecret string
	log       logger.Logger
}

// NewAuthUseCase creates a new AuthUseCase implementation.
func NewAuthUseCase(
	userRepo domain.UserRepository,
	sf *snowflake.Generator,
	jwtSecret string,
) (AuthUseCase, error) {
	if userRepo == nil {
		return nil, fmt.Errorf("userRepo is required")
	}
	if sf == nil {
		return nil, fmt.Errorf("snowflake generator is required")
	}
	if jwtSecret == "" {
		return nil, fmt.Errorf("jwtSecret is required")
	}
	return &authUseCase{
		userRepo:  userRepo,
		snowflake: sf,
		jwtSecret: jwtSecret,
		log:       logger.For("usecase", "auth"),
	}, nil
}

// RegisterUser creates a user if they don't exist, or retrieves them if they do,
// then returns a JWT. Called by external auth services to sync users into Nexus.
func (uc *authUseCase) RegisterUser(ctx context.Context, appID, email, name string) (string, string, error) {
	if email == "" {
		return "", "", fmt.Errorf("register: email is required")
	}

	user, err := uc.userRepo.GetByEmail(ctx, appID, email)
	if err != nil {
		return "", "", fmt.Errorf("register: %w", err)
	}

	if user == nil {
		id, err := uc.snowflake.NextID()
		if err != nil {
			return "", "", fmt.Errorf("register: generate user id: %w", err)
		}
		userID := fmt.Sprintf("%d", id)
		privateTopic := fmt.Sprintf("nexus.%s.v1.user.%s", appID, userID)

		user = &domain.UserProfile{
			ID:           userID,
			AppID:        appID,
			Email:        email,
			Name:         name,
			PrivateTopic: privateTopic,
		}
		if err := uc.userRepo.Create(ctx, user); err != nil {
			return "", "", fmt.Errorf("register: create user: %w", err)
		}
		uc.log.Infof("User registered via internal API: %s/%s (%s)", appID, userID, email)
	}

	token, err := uc.generateJWT(appID, user.ID)
	if err != nil {
		return "", "", fmt.Errorf("register: %w", err)
	}

	return token, user.ID, nil
}

// generateJWT creates an HS256-signed JWT with sub (userID) and tid (appID) claims, valid for 24h.
func (uc *authUseCase) generateJWT(appID, userID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": userID,
		"tid": appID,
		"iat": now.Unix(),
		"exp": now.Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(uc.jwtSecret))
}
