-- user_groups: Tracks which users belong to which groups (per app).

CREATE TABLE IF NOT EXISTS user_groups (
    app_id    VARCHAR(64)  NOT NULL,
    user_id   VARCHAR(64)  NOT NULL,
    group_id  VARCHAR(128) NOT NULL,
    joined_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (app_id, user_id, group_id),
    INDEX idx_user_groups_group (app_id, group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
