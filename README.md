<div align="center">
	<h1>Nexus</h1>
	<p><strong>Scalable WebSocket messaging service for chat applications</strong></p>
</div>

<div align="center">

[![codecov](https://codecov.io/gh/obrel/nexus/graph/badge.svg?token=AOV2WH1NRK)](https://codecov.io/gh/obrel/nexus)
[![Go Test](https://github.com/obrel/nexus/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/obrel/nexus/actions/workflows/go.yml)
[![go-reportcard](https://goreportcard.com/badge/github.com/obrel/nexus)](https://goreportcard.com/report/github.com/obrel/nexus)

</div>

---

Nexus is a distributed messaging backend that handles the hard parts of real-time chat: reliable delivery, group management, presence tracking, and horizontal scaling. It uses a CQRS-inspired architecture where reads and writes flow through separate tiers, connected by NATS.

**You bring your own auth service. Nexus handles the messaging.**

## How It Works

```
               HTTP :8080                         WS :8081
           ┌──────────────┐                  ┌──────────────┐
 Client ──>│   Ingress    │──┐          ┌───>│    Egress    │──> Client
           │  (stateless) │  │          │    │  (stateful)  │
           └──────────────┘  │  ┌─────┐ │    └──────────────┘
                             ├─>│NATS │─┤
           ┌──────────────┐  │  └─────┘ │
           │    Worker    │──┘          │
           │(outbox relay)│             │
           └──────────────┘             │
                                        │
           ┌──────────────┐             │
           │    MySQL     │ messages,   │
           │    Redis     │ presence    │
           └──────────────┘             │
```

- **Ingress** accepts HTTP requests: send messages, manage groups, update profiles.
- **Worker** polls the transactional outbox and publishes to NATS (guaranteed delivery).
- **Egress** holds WebSocket connections and pushes real-time events to users.
- **MySQL** stores messages (daily-partitioned), groups, and user data.
- **Redis** manages presence, metadata, and rate limiting.

All three tiers scale independently. Ingress is stateless. Egress is stateful but uses shard-based NATS subscriptions for efficient fan-out.

## Features

- **Group & Private Messaging** - Send to groups or directly to users, with membership enforcement
- **Transactional Outbox** - Messages are never lost, even if NATS is temporarily down
- **Snowflake IDs** - Globally unique, time-ordered, no coordination needed
- **Consistent Hashing** - Groups map to shards for balanced NATS subject distribution
- **Presence & Heartbeat** - Real-time online/offline tracking via Redis with TTL
- **Multi-tenancy** - Multiple apps on one deployment, isolated by `app_id`
- **JWT Authentication** - Stateless auth with `sub` (user) and `tid` (tenant) claims
- **Message Lifecycle** - Send, edit, soft-delete, acknowledge (read receipts)
- **History Sync** - Cursor-based pagination for catching up on missed messages

## Documentation

Full API reference, authentication guide, JWT code snippets, and configuration details are available in the **[Documentation](https://nexus.docs.web.id/docs.html)**.

When the server is running, Swagger UI is available at `http://localhost:8080/docs` and the raw OpenAPI spec at `http://localhost:8080/docs/openapi.yaml`.

## Quick Start

### Prerequisites

- Go 1.24+
- MySQL 8.0+
- Redis 6+
- NATS 2.9+

### 1. Configure

```bash
cp config.yaml.example config.yaml
# Edit config.yaml with your MySQL, Redis, and NATS credentials
```

### 2. Run Migrations

```bash
go run ./cmd/nexus migrate up
```

### 3. Start All Three Tiers

Open three terminals:

```bash
# Terminal 1 - HTTP Ingress
go run ./cmd/nexus http

# Terminal 2 - WebSocket Egress
go run ./cmd/nexus ws

# Terminal 3 - Outbox Relay Worker
go run ./cmd/nexus worker
```

### 4. Try It Out

Register a user via the internal API:

```bash
curl -X POST http://localhost:8080/internal/v1/auth/register \
  -H "X-Internal-Key: dev-internal-key-change-in-production" \
  -H "Content-Type: application/json" \
  -d '{"email": "alice@example.com", "name": "Alice"}'
```

Send a message:

```bash
curl -X POST http://localhost:8080/v1/messages/send \
  -H "Authorization: Bearer <token-from-register>" \
  -H "Content-Type: application/json" \
  -d '{"group_id": "general", "content": {"type": "text", "text": "Hello!"}}'
```

### Docker Compose

If you prefer containers (includes all dependencies: MySQL, Redis, NATS, and automatic migrations):

```bash
docker compose up --build
```

## Example Client

A ready-to-use browser client is included for testing:

```
examples/client/index.html
```

Open it in two browser tabs, register as different users, join the same group, and watch messages flow in real time.

## Project Structure

For a detailed breakdown of the codebase, see the **[Documentation](pages/docs.html)**.

## Authentication

Nexus uses **JWT** for authentication but does **not** provide login or registration flows. You implement auth in your own service and sync users into Nexus via the internal API:

1. User logs in through **your** auth service
2. Your service calls `POST /internal/v1/auth/register` to create/retrieve the user in Nexus
3. Nexus returns a JWT - pass it to the client
4. Client uses the JWT for all Nexus API calls and WebSocket connections

JWT claims: `sub` (user ID), `tid` (app/tenant ID), `iat`, `exp` (24h).

See the [documentation](https://nexus.docs.web.id/docs.html) for code snippets on generating JWTs in Go, Node.js, and Python.

## Database

See [db/README.md](db/README.md) for migration details and schema overview.

Key design choices:
- **Daily partitioning** on the messages table for efficient archival and queries
- **Transactional outbox** ensures messages are published to NATS exactly once
- **Snowflake IDs** enable cursor-based pagination without timestamp ambiguity

## License

MIT License - see [LICENSE](LICENSE) for details.
