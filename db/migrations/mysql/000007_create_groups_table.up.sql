-- groups: Group/channel metadata with shard assignment.

CREATE TABLE IF NOT EXISTS `groups` (
    group_id    VARCHAR(128)  NOT NULL,
    app_id      VARCHAR(64)   NOT NULL,
    name        VARCHAR(255)  NOT NULL DEFAULT '',
    description TEXT          NOT NULL,
    shard_id    INT           NOT NULL,
    created_by  VARCHAR(64)   NOT NULL,
    created_at  TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (app_id, group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
