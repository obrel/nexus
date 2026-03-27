-- messages: Chat messages with polymorphic recipient and sender.
-- Partitioned by day on created_at for efficient time-range queries and pruning.

CREATE TABLE IF NOT EXISTS messages (
    id             BIGINT UNSIGNED NOT NULL,
    app_id         VARCHAR(100)    NOT NULL,
    shard          INT UNSIGNED    NOT NULL,
    recipient_type ENUM('group', 'user') NOT NULL DEFAULT 'group',
    recipient_id   VARCHAR(255)    NOT NULL,
    sender_type    ENUM('user')    NOT NULL DEFAULT 'user',
    sender_id      VARCHAR(255)    NOT NULL,
    content        JSON            NOT NULL,
    created_at     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    status         ENUM('sent', 'received', 'read', 'deleted') NOT NULL DEFAULT 'sent',
    metadata       JSON,

    PRIMARY KEY (id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
PARTITION BY RANGE (YEAR(created_at) * 10000 + MONTH(created_at) * 100 + DAY(created_at)) (
    PARTITION p20260101 VALUES LESS THAN (20260102),
    PARTITION p20260102 VALUES LESS THAN (20260103),
    PARTITION p20260103 VALUES LESS THAN (20260104),
    PARTITION p20260104 VALUES LESS THAN (20260105),
    PARTITION p20260105 VALUES LESS THAN (20260106),
    PARTITION p20260106 VALUES LESS THAN (20260107),
    PARTITION p20260107 VALUES LESS THAN (20260108),
    PARTITION pmax VALUES LESS THAN MAXVALUE
);

CREATE INDEX idx_messages_app_shard_created     ON messages(app_id, shard, created_at);
CREATE INDEX idx_messages_app_sender_created    ON messages(app_id, sender_id, created_at);
CREATE INDEX idx_messages_app_recipient_created ON messages(app_id, recipient_id, created_at);
CREATE INDEX idx_messages_app_type_recipient    ON messages(app_id, recipient_type, recipient_id, created_at);
CREATE INDEX idx_messages_app_sender_recipient  ON messages(app_id, sender_id, recipient_id, created_at);
