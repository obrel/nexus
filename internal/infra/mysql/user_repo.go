package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// UserRepository implements domain.UserRepository for MySQL,
// operating on the existing `users` table.
type UserRepository struct {
	db  *sql.DB
	log logger.Logger
}

// NewUserRepository creates a UserRepository backed by db.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{
		db:  db,
		log: logger.For("infra", "mysql"),
	}
}

// GetByID fetches a user profile by app_id and user_id.
func (r *UserRepository) GetByID(ctx context.Context, appID, userID string) (*domain.UserProfile, error) {
	const q = `
		SELECT id, app_id, email, name, bio, private_topic, created_at
		FROM users
		WHERE app_id = ? AND id = ?`
	p := &domain.UserProfile{}
	err := r.db.QueryRowContext(ctx, q, appID, userID).Scan(
		&p.ID, &p.AppID, &p.Email, &p.Name, &p.Bio, &p.PrivateTopic, &p.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user_repo: get %s/%s: %w", appID, userID, err)
	}
	return p, nil
}

// GetByEmail fetches a user profile by app_id and email.
func (r *UserRepository) GetByEmail(ctx context.Context, appID, email string) (*domain.UserProfile, error) {
	const q = `
		SELECT id, app_id, email, name, bio, private_topic, created_at
		FROM users
		WHERE app_id = ? AND email = ?`
	p := &domain.UserProfile{}
	err := r.db.QueryRowContext(ctx, q, appID, email).Scan(
		&p.ID, &p.AppID, &p.Email, &p.Name, &p.Bio, &p.PrivateTopic, &p.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("user_repo: get_by_email %s/%s: %w", appID, email, err)
	}
	return p, nil
}

// Create inserts a new user profile.
func (r *UserRepository) Create(ctx context.Context, profile *domain.UserProfile) error {
	const q = `
		INSERT INTO users (id, app_id, email, name, bio, private_topic)
		VALUES (?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		profile.ID, profile.AppID, profile.Email, profile.Name, profile.Bio, profile.PrivateTopic,
	)
	if err != nil {
		return fmt.Errorf("user_repo: create %s/%s: %w", profile.AppID, profile.ID, err)
	}
	return nil
}

// Update modifies the name and bio fields of an existing user.
func (r *UserRepository) Update(ctx context.Context, appID, userID string, profile *domain.UserProfile) error {
	const q = `UPDATE users SET name = ?, bio = ? WHERE app_id = ? AND id = ?`
	res, err := r.db.ExecContext(ctx, q, profile.Name, profile.Bio, appID, userID)
	if err != nil {
		return fmt.Errorf("user_repo: update %s/%s: %w", appID, userID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user_repo: update %s/%s: not found", appID, userID)
	}
	return nil
}
