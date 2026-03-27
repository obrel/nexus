-- users: User profiles with email authentication support.

CREATE TABLE IF NOT EXISTS users (
    id            VARCHAR(64)  NOT NULL,
    app_id        VARCHAR(64)  NOT NULL,
    email         VARCHAR(255) NOT NULL DEFAULT '',
    name          VARCHAR(128) NOT NULL DEFAULT '',
    bio           TEXT,
    private_topic VARCHAR(255) NOT NULL DEFAULT '',
    created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (app_id, id),
    UNIQUE INDEX idx_users_email (app_id, email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
