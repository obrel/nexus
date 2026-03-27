# Database Migrations

This directory contains database migrations managed by [golang-migrate](https://github.com/golang-migrate/migrate).

## Prerequisites

- MySQL 8.0+
- Database created: `CREATE DATABASE IF NOT EXISTS nexus;`
- Config file with MySQL credentials (see `config.yaml.example`)

## Running Migrations

From the nexus root directory:

```bash
# Run all pending migrations
go run ./cmd/nexus migrate up

# Show current migration version
go run ./cmd/nexus migrate version

# Rollback the last migration
go run ./cmd/nexus migrate down
```

Or with a compiled binary:

```bash
./nexus migrate up
```

## Migrations

| # | Migration | Description |
|---|-----------|-------------|
| 1 | `create_messages_table` | Chat messages with daily partitioning |
| 2 | `create_outbox_table` | Transactional outbox for NATS relay |
| 3 | `create_user_groups_table` | User-to-group membership |
| 4 | `create_message_status_table` | Per-user delivery state tracking |
| 5 | `create_users_table` | User profiles |
| 6 | `create_otp_verifications_table` | OTP verification (deprecated) |
| 7 | `create_groups_table` | Group metadata with shard assignment |

## Schema Overview

### `messages`

Stores all chat messages (group and private). Partitioned daily by `created_at` using `RANGE` partitioning for efficient time-range queries and data archival.

Key columns: `id` (Snowflake), `app_id`, `shard`, `recipient_type` (group/user), `recipient_id`, `sender_id`, `content` (JSON), `status`.

### `outbox`

Implements the **transactional outbox pattern**. When a message is sent, both the message row and an outbox row are inserted in the same transaction. The relay worker polls this table and publishes to NATS.

Status flow: `pending` -> `published` (or `failed` after retries).

### `groups`

Group/channel metadata. Each group has a `shard_id` computed via consistent hashing, which determines its NATS subject for message fan-out.

### `user_groups`

Membership join table. Composite primary key `(app_id, user_id, group_id)` ensures a user can only join a group once per tenant.

### `message_status`

Tracks per-user delivery state for read receipts. Status values: `sent`, `received`, `read`.

### `users`

User profiles with email uniqueness per tenant. The `private_topic` column stores the user's NATS subject for direct messages.

## Partition Management

The initial migration creates daily partitions for early 2026 plus a `pmax` catchall. For production, partitions should be created ahead of time and old ones archived periodically.

## Configuration

Database connection in `config.yaml`:

```yaml
mysql:
  host: localhost
  port: 3306
  user: root
  password: password
  dbname: nexus
  max_open_conns: 100
  max_idle_conns: 25
  conn_max_lifetime: 300
```

Environment variable overrides follow the pattern `NEXUS_MYSQL_HOST`, `NEXUS_MYSQL_PORT`, etc.
