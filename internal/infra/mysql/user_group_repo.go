package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/obrel/nexus/internal/infra/logger"
)

// UserGroupRepository implements domain.UserGroupRepository for MySQL.
type UserGroupRepository struct {
	db  *sql.DB
	log logger.Logger
}

// NewUserGroupRepository creates a UserGroupRepository backed by db.
func NewUserGroupRepository(db *sql.DB) *UserGroupRepository {
	return &UserGroupRepository{
		db:  db,
		log: logger.For("infra", "mysql"),
	}
}

// JoinGroup adds a user to a group. Duplicate joins are silently ignored.
func (r *UserGroupRepository) JoinGroup(ctx context.Context, appID, userID, groupID string) error {
	const q = `INSERT IGNORE INTO user_groups (app_id, user_id, group_id) VALUES (?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, appID, userID, groupID)
	if err != nil {
		return fmt.Errorf("user_group_repo: join %s/%s group %s: %w", appID, userID, groupID, err)
	}
	return nil
}

// LeaveGroup removes a user from a group.
func (r *UserGroupRepository) LeaveGroup(ctx context.Context, appID, userID, groupID string) error {
	const q = `DELETE FROM user_groups WHERE app_id = ? AND user_id = ? AND group_id = ?`
	_, err := r.db.ExecContext(ctx, q, appID, userID, groupID)
	if err != nil {
		return fmt.Errorf("user_group_repo: leave %s/%s group %s: %w", appID, userID, groupID, err)
	}
	return nil
}

// GetUserGroups returns all group IDs a user belongs to.
func (r *UserGroupRepository) GetUserGroups(ctx context.Context, appID, userID string) ([]string, error) {
	const q = `SELECT group_id FROM user_groups WHERE app_id = ? AND user_id = ?`
	rows, err := r.db.QueryContext(ctx, q, appID, userID)
	if err != nil {
		return nil, fmt.Errorf("user_group_repo: get_user_groups %s/%s: %w", appID, userID, err)
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			return nil, fmt.Errorf("user_group_repo: scan: %w", err)
		}
		groups = append(groups, gid)
	}
	return groups, rows.Err()
}

// IsMember checks if a user belongs to a group.
func (r *UserGroupRepository) IsMember(ctx context.Context, appID, userID, groupID string) (bool, error) {
	const q = `SELECT 1 FROM user_groups WHERE app_id = ? AND user_id = ? AND group_id = ? LIMIT 1`
	var exists int
	err := r.db.QueryRowContext(ctx, q, appID, userID, groupID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("user_group_repo: is_member %s/%s group %s: %w", appID, userID, groupID, err)
	}
	return true, nil
}
