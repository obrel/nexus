package usecase

import (
	"context"
	"fmt"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// profileUseCase implements ProfileUseCase for user profile operations.
type profileUseCase struct {
	userRepo domain.UserRepository
	log      logger.Logger
}

// NewProfileUseCase creates a new ProfileUseCase implementation.
func NewProfileUseCase(userRepo domain.UserRepository) (ProfileUseCase, error) {
	if userRepo == nil {
		return nil, fmt.Errorf("userRepo is required")
	}
	return &profileUseCase{
		userRepo: userRepo,
		log:      logger.For("usecase", "profile"),
	}, nil
}

// Get returns the user profile for the authenticated user.
func (uc *profileUseCase) Get(ctx context.Context, appID, userID string) (*domain.UserProfile, error) {
	profile, err := uc.userRepo.GetByID(ctx, appID, userID)
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	if profile == nil {
		return nil, fmt.Errorf("get profile: user %s/%s not found", appID, userID)
	}
	return profile, nil
}

// Update modifies the user's name and bio.
func (uc *profileUseCase) Update(ctx context.Context, appID, userID string, profile *domain.UserProfile) error {
	if err := uc.userRepo.Update(ctx, appID, userID, profile); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	uc.log.Infof("Profile updated: %s/%s", appID, userID)
	return nil
}
