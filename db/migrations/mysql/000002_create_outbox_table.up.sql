-- outbox: Transactional outbox for reliable NATS delivery.
-- Polled by the relay worker; payload stored inline to avoid JOIN on partitioned messages table.
-- data_type distinguishes message events from custom events (polymorphic).
-- data_id holds the message ID for message rows; NULL for custom event rows.

CREATE TABLE IF NOT EXISTS outbox (
    id           BIGINT UNSIGNED  NOT NULL PRIMARY KEY,
    app_id       VARCHAR(100)     NOT NULL,
    data_type    ENUM('message', 'event') NOT NULL DEFAULT 'message',
    data_id      BIGINT UNSIGNED  NULL,
    status       ENUM('pending', 'published', 'failed') NOT NULL DEFAULT 'pending',
    retry_count  INT UNSIGNED     NOT NULL DEFAULT 0,
    last_error   TEXT,
    created_at   DATETIME(3)      NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    published_at DATETIME(3),
    nats_subject VARCHAR(500)     NOT NULL,
    payload      MEDIUMTEXT       NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_outbox_app_status_created ON outbox(app_id, status, created_at);
CREATE INDEX idx_outbox_status_created     ON outbox(status, created_at);
