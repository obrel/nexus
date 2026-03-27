package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// GroupRepository implements domain.GroupRepository for MySQL.
type GroupRepository struct {
	db  *sql.DB
	log logger.Logger
}

// NewGroupRepository creates a GroupRepository backed by db.
func NewGroupRepository(db *sql.DB) *GroupRepository {
	return &GroupRepository{
		db:  db,
		log: logger.For("infra", "mysql"),
	}
}

// Create inserts a new group record. Returns an error if the group already exists.
func (r *GroupRepository) Create(ctx context.Context, group *domain.Group) error {
	const q = `INSERT INTO ` + "`groups`" + ` (group_id, app_id, name, description, shard_id, created_by) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, group.ID, group.AppID, group.Name, group.Description, group.ShardID, group.CreatedBy)
	if err != nil {
		return fmt.Errorf("group_repo: create %s/%s: %w", group.AppID, group.ID, err)
	}
	return nil
}

// GetGroup fetches a group by app ID and group ID. Returns nil if not found.
func (r *GroupRepository) GetGroup(ctx context.Context, appID, groupID string) (*domain.Group, error) {
	const q = `SELECT group_id, app_id, name, description, shard_id, created_by, created_at FROM ` + "`groups`" + ` WHERE app_id = ? AND group_id = ?`
	var g domain.Group
	err := r.db.QueryRowContext(ctx, q, appID, groupID).Scan(&g.ID, &g.AppID, &g.Name, &g.Description, &g.ShardID, &g.CreatedBy, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("group_repo: get %s/%s: %w", appID, groupID, err)
	}
	return &g, nil
}

// Update modifies the name and description of an existing group.
func (r *GroupRepository) Update(ctx context.Context, appID, groupID, name, description string) error {
	const q = `UPDATE ` + "`groups`" + ` SET name = ?, description = ? WHERE app_id = ? AND group_id = ?`
	res, err := r.db.ExecContext(ctx, q, name, description, appID, groupID)
	if err != nil {
		return fmt.Errorf("group_repo: update %s/%s: %w", appID, groupID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("group_repo: update %s/%s: group not found", appID, groupID)
	}
	return nil
}

// GetGroupMembers returns all user IDs that belong to the given group.
func (r *GroupRepository) GetGroupMembers(ctx context.Context, appID, groupID string) ([]string, error) {
	const q = `SELECT user_id FROM user_groups WHERE app_id = ? AND group_id = ?`
	rows, err := r.db.QueryContext(ctx, q, appID, groupID)
	if err != nil {
		return nil, fmt.Errorf("group_repo: get_members %s/%s: %w", appID, groupID, err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("group_repo: scan: %w", err)
		}
		members = append(members, uid)
	}
	return members, rows.Err()
}
