-- message_status: Per-user delivery state tracking (sent/received/read).

CREATE TABLE IF NOT EXISTS message_status (
    message_id BIGINT UNSIGNED NOT NULL,
    app_id     VARCHAR(64)     NOT NULL,
    user_id    VARCHAR(64)     NOT NULL,
    status     VARCHAR(16)     NOT NULL DEFAULT 'sent',
    updated_at TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (message_id, app_id, user_id),
    INDEX idx_message_status_user (app_id, user_id, message_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
