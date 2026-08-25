# Project Plan

## Table of Contents

1. [Overview](#overview)
2. [Tech Stack](#tech-stack)
3. [API Routes](#api-routes)
4. [Database Schema](#database-schema)

---

## Overview

**Gater** is a backend API for event ticketing. Create events, manage ticket inventory, handle purchases, and check in attendees via QR code. Built to handle the hard parts: concurrent ticket purchases, waitlist promotion, and state management across the full event lifecycle. Built with Go, PostgreSQL, and Redis.

---

## Tech Stack

| Concern          | Choice                                        |
| ---------------- | --------------------------------------------- |
| HTTP             | Chi router                                    |
| Database         | PostgreSQL + `pgx`                            |
| Cache + Queue    | Redis                                         |
| Background Jobs  | Asynq                                         |
| Password Hashing | Argon2id (`golang.org/x/crypto/argon2`)       |
| Email            | Resend                                        |
| OAuth            | Google OAuth 2.0                              |
| QR Token Signing | HMAC-SHA256                                   |
| Currency         | USD (cents)                                   |
| Live Reloading   | Air                                           |
| Deployment       | Docker + Dokploy                              |
| DNS / CDN        | Cloudflare                                    |
| API Docs         | Scalar UI + OpenAPI (generation approach TBD) |

---

## API Routes

### Auth — `/api/auth`

| Method | Path                            | Auth      | Description                     |
| ------ | ------------------------------- | --------- | ------------------------------- |
| `POST` | `/api/auth/register`            | Public    | Register with email + password  |
| `POST` | `/api/auth/login`               | Public    | Login with email + password     |
| `POST` | `/api/auth/logout`              | Protected | End session                     |
| `GET`  | `/api/auth/me`                  | Protected | Get current user                |
| `POST` | `/api/auth/verify-email`        | Public    | Submit email verification token |
| `POST` | `/api/auth/resend-verification` | Public    | Resend verification email       |
| `POST` | `/api/auth/forgot-password`     | Public    | Send password reset email       |
| `POST` | `/api/auth/reset-password`      | Public    | Reset password via token        |
| `GET`  | `/api/auth/google`              | Public    | Initiate Google OAuth flow      |
| `GET`  | `/api/auth/google/callback`     | Public    | Google OAuth callback           |
| `POST` | `/api/auth/become-organizer`    | Protected | Switch role to organizer        |

### Events — `/api/events`

| Method   | Path                         | Auth      | Description                                   |
| -------- | ---------------------------- | --------- | --------------------------------------------- |
| `POST`   | `/api/events`                | Organizer | Create an event                               |
| `GET`    | `/api/events`                | Public    | List all published events (paginated)         |
| `GET`    | `/api/events/{id}`           | Public    | Get a single event's details (drafts private) |
| `PATCH`  | `/api/events/{id}`           | Organizer | Update an event (ownership)                   |
| `DELETE` | `/api/events/{id}`           | Organizer | Delete an event (draft only)                  |
| `POST`   | `/api/events/{id}/publish`   | Organizer | Publish an event                              |
| `POST`   | `/api/events/{id}/cancel`    | Organizer | Cancel an event                               |
| `GET`    | `/api/events/{id}/analytics` | Organizer | Get check-in + purchase analytics             |

**Create request:**

```json
{
  "name": "Lagos Tech Meetup",
  "description": "A gathering of tech folks in Lagos.",
  "location": "Landmark Event Centre, Lagos",
  "starts_at": "2026-08-15T18:00:00Z",
  "ends_at": "2026-08-15T21:00:00Z",
  "capacity": 300,
  "cancellation_policy": {
    "allowed": true,
    "hours_before": 24
  },
  "max_tickets_per_purchase": 5
}
```

`capacity` is optional. If set, the sum of all tier quantities can never exceed it.

**Event response:**

```json
{
  "id": "uuid",
  "name": "Lagos Tech Meetup",
  "description": "A gathering of tech folks in Lagos.",
  "location": "Landmark Event Centre, Lagos",
  "status": "published",
  "starts_at": "2026-08-15T18:00:00Z",
  "ends_at": "2026-08-15T21:00:00Z",
  "capacity": 300,
  "cancellation_allowed": true,
  "cancellation_hours_before": 24,
  "max_tickets_per_purchase": 5,
  "organizer": {
    "id": "uuid",
    "name": "Goziem"
  },
  "tiers": [...],
  "created_at": "2026-05-26T10:00:00Z"
}
```

**Valid event status transitions:**

```
Draft      → Published  (manual, organizer)
Draft      → Cancelled  (manual, organizer)
Published  → Cancelled  (manual, organizer)
Published  → Sold Out   (automatic, triggered by inventory)
Sold Out   → Cancelled  (manual, organizer)
Sold Out   → Published  (automatic, triggered by cancellation freeing inventory or a tier quantity increase)
Published  → Ended      (automatic, triggered by event date passing)
Sold Out   → Ended      (automatic, triggered by event date passing)
```

Invalid transitions are rejected with a `409 Conflict`.

**Visibility (implemented):** draft/cancelled events return `404` to non-organizers (no existence leak). Handlers running behind `maybeAuth` recognize the organizer via `publicEvent`.

**Update policy (implemented):**

- Draft events: fully editable.
- Published / sold_out: material fields (`starts_at`, `ends_at`, `location`, `cancellation_allowed`, `cancellation_hours_before`) require `confirm_material_change: true` in the request, else `409` (with the number of confirmed ticket holders affected in the message). Capacity cannot be lowered below tickets already sold. Name, description, `max_tickets_per_purchase` editable anytime.
- A confirmed material change on a published/sold_out event stamps `material_changed_at`, opening a **72h cancellation grace window** for existing buyers — they may cancel even after the normal window closes (the window is effectively capped at the event start, since cancellation is impossible once it begins). Cosmetic-only updates never move the stamp.
- Cancelled / ended: frozen, `409`.
- Publish requires at least one tier.

### Ticket Tiers — `/api/events/{id}/tiers`

| Method   | Path                              | Auth      | Description                |
| -------- | --------------------------------- | --------- | -------------------------- |
| `POST`   | `/api/events/{id}/tiers`          | Organizer | Create a ticket tier       |
| `GET`    | `/api/events/{id}/tiers`          | Public    | List tiers for an event    |
| `PATCH`  | `/api/events/{id}/tiers/{tierId}` | Organizer | Update a tier (ownership)  |
| `DELETE` | `/api/events/{id}/tiers/{tierId}` | Organizer | Delete a tier (draft only) |

**Status (implemented):** all four endpoints are done. Create/list run behind `maybeAuth` — tier listings follow the same draft/cancelled visibility rule as events.

**Tier update rules (implemented):**

- `name` and `price` are editable anytime (historical purchase totals are stored per purchase, so past buyers are unaffected)
- `quantity` can move in any direction but must stay `>= sold` (`sold = old quantity - remaining`) — `422` otherwise; `remaining` is recomputed as `new quantity - sold`
- Capacity check nets out the tier's old quantity — sum of all tier quantities must not exceed event capacity (`400` otherwise)
- `sold_out → available` flip: a tier regains inventory whenever `remaining > 0` after an update
- `sold_out → published` flip: an event that was fully sold out returns to `published` when any tier regains inventory
- Delete is locked to draft events (`409` otherwise) — deleting a published tier could strand purchases
- Cancelled/ended events: all tier edits → `409` (frozen, matches event policy)

**Create tier request:**

```json
{
  "name": "General Admission",
  "price": 2000,
  "quantity": 200
}
```

Price in cents. If the event has a `capacity` set, the sum of all tier quantities including this new one must not exceed it — returns `400` with a descriptive error if exceeded.

**Tier response:**

```json
{
  "id": "uuid",
  "name": "General Admission",
  "price": 2000,
  "quantity": 200,
  "remaining": 200,
  "status": "available",
  "created_at": "2026-05-26T10:00:00Z"
}
```

### Purchases — `/api/purchases`

| Method | Path                         | Auth     | Description                      |
| ------ | ---------------------------- | -------- | -------------------------------- |
| `POST` | `/api/purchases`             | Attendee | Purchase tickets                 |
| `GET`  | `/api/purchases`             | Attendee | List your purchases              |
| `GET`  | `/api/purchases/{id}`        | Attendee | Get a single purchase + QR codes |
| `POST` | `/api/purchases/{id}/cancel` | Attendee | Cancel a purchase                |

**Purchase request:**

```json
{
  "tier_id": "uuid",
  "quantity": 2
}
```

**Purchase response:** the purchase nests **full** `event`, `tier`, and `tickets` objects (all columns), not summaries:

```json
{
  "id": "uuid",
  "quantity": 2,
  "total": 4000,
  "status": "confirmed",
  "event": {
    "id": "uuid",
    "organizer_id": "uuid",
    "name": "Lagos Tech Meetup",
    "description": "A gathering of tech folks in Lagos.",
    "location": "Landmark Event Centre, Lagos",
    "status": "published",
    "starts_at": "2026-08-15T18:00:00Z",
    "ends_at": "2026-08-15T21:00:00Z",
    "capacity": 300,
    "cancellation_allowed": true,
    "cancellation_hours_before": 24,
    "max_tickets_per_purchase": 5,
    "created_at": "2026-05-26T10:00:00Z",
    "updated_at": "2026-05-26T10:00:00Z"
  },
  "tier": {
    "id": "uuid",
    "event_id": "uuid",
    "name": "General Admission",
    "price": 2000,
    "quantity": 200,
    "remaining": 198,
    "status": "available",
    "created_at": "2026-05-26T10:00:00Z",
    "updated_at": "2026-05-26T10:00:00Z"
  },
  "tickets": [
    {
      "id": "uuid",
      "purchase_id": "uuid",
      "tier_id": "uuid",
      "qr_token": "signed-token-here",
      "status": "unused",
      "checked_in_at": null,
      "created_at": "2026-05-26T10:00:00Z",
      "updated_at": "2026-05-26T10:00:00Z"
    }
  ],
  "created_at": "2026-05-26T10:00:00Z",
  "updated_at": "2026-05-26T10:00:00Z"
}
```

**Cancellation rules (enforced at purchase time):**

- Event must not have started
- `cancellation_allowed` must be `true` on the event
- Valid cancellation window: within `cancellation_hours_before` hours of `starts_at`, OR within 72h of the event's last `material_changed_at` stamp (grace window for buyers whose deal changed)
- Cancelling against an already-cancelled event is allowed; once started or ended it never is
- On cancellation: purchase and its tickets flip to `cancelled`, inventory is restored to the tier, and a fully sold-out event reopens as `published`; waitlist promotion is triggered

### Waitlist — `/api/events/{id}/tiers/{tierId}/waitlist`

The waitlist is for tiers that are sold out. A user either purchases a ticket or joins the waitlist — never both for the same tier simultaneously.

| Method   | Path                                       | Auth      | Description                     |
| -------- | ------------------------------------------ | --------- | ------------------------------- |
| `POST`   | `/api/events/{id}/tiers/{tierId}/waitlist` | Attendee  | Join the waitlist for a tier    |
| `DELETE` | `/api/events/{id}/tiers/{tierId}/waitlist` | Attendee  | Leave the waitlist              |
| `GET`    | `/api/events/{id}/waitlist`                | Organizer | View full waitlist for an event |

**Status (implemented):** all three entry-management endpoints are done.

**Join rules:** event must be publicly visible (draft/cancelled 404 to non-organizers) and not ended; tier must belong to the route's event, be `sold_out` (`409` otherwise — buying beats queueing), and the user must not already hold confirmed tickets for it (`409`). `UNIQUE(user_id, tier_id)` allows one entry per user per tier — a duplicate join is a `409`, and **expired entries block rejoining until Phase 12** adds conditional reset (the expired state can't occur before promotion exists, so strict v1 costs nothing).

**Leave rules:** hard delete of the caller's own `(user_id, tier_id)` row — pair-scoping collapses missing/foreign/nonexistent into one `404`; leaving a cancelled/ended event's waitlist stays allowed. Deleting frees the UNIQUE slot.

**Organizer view:** FIFO across all tiers of the event (`created_at ASC`, matching future promotion order); each row carries buyer name/email, tier name, status, and `notified_at`/`expires_at` so organizers can see who holds a live offer.

**Waitlist promotion flow (Phase 12):**

1. Purchase is cancelled, inventory restored
2. `NotifyWaitlistEntry` job enqueued via Asynq
3. Worker finds the next `waiting` entry for the tier (ordered by `created_at`)
4. Sets entry status to `notified`, sets `notified_at` and `expires_at` (24 hours)
5. Sends email to the user with a link to complete their purchase
6. If user purchases in time: entry status → `purchased`
7. If `expires_at` passes without purchase: `ExpireWaitlistReservations` worker sets status → `expired`, promotes next person

### Check-in — `/api/checkin`

| Method | Path           | Auth      | Description                    |
| ------ | -------------- | --------- | ------------------------------ |
| `POST` | `/api/checkin` | Organizer | Check in a ticket via QR token |

**Check-in request:**

```json
{
  "token": "signed-qr-token-here"
}
```

**Success response:**

```json
{
  "valid": true,
  "attendee": {
    "name": "John Doe",
    "email": "john@example.com"
  },
  "ticket": {
    "id": "uuid",
    "tier": "General Admission",
    "checked_in_at": "2026-08-15T18:32:00Z"
  }
}
```

**Error response:**

```json
{
  "valid": false,
  "reason": "already checked in"
}
```

Possible reasons: `already checked in`, `ticket cancelled`, `invalid token`, `wrong event`.

The check-in lookup and status update happen in a single transaction with `SELECT FOR UPDATE` — two scanners at different doors cannot both successfully check in the same ticket.

### Organizer Events — `/api/organizer/events`

| Method | Path                    | Auth      | Description                         |
| ------ | ----------------------- | --------- | ----------------------------------- |
| `GET`  | `/api/organizer/events` | Organizer | List all your events (all statuses) |

### API Docs

| Method | Path                | Description      |
| ------ | ------------------- | ---------------- |
| `GET`  | `/api/docs`         | Scalar UI        |
| `GET`  | `/api/openapi.yaml` | Raw OpenAPI spec |

---

## Database Schema

### `users`

```sql
CREATE TABLE users (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name           TEXT NOT NULL,
  email          TEXT NOT NULL UNIQUE,
  password_hash  TEXT,                        -- NULL for OAuth-only accounts
  email_verified BOOLEAN NOT NULL DEFAULT FALSE,
  image          TEXT,
  role           TEXT NOT NULL DEFAULT 'attendee', -- 'attendee' or 'organizer'
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### `sessions`

```sql
CREATE TABLE sessions (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token        TEXT NOT NULL UNIQUE,
  ip_address   TEXT,
  user_agent   TEXT,
  expires_at   TIMESTAMPTZ NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sessions_token ON sessions(token);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
```

### `oauth_accounts`

```sql
CREATE TABLE oauth_accounts (
  id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider                 TEXT NOT NULL,
  provider_account_id      TEXT NOT NULL,
  access_token             TEXT,
  refresh_token            TEXT,
  id_token                 TEXT,
  access_token_expires_at  TIMESTAMPTZ,
  refresh_token_expires_at TIMESTAMPTZ,
  scope                    TEXT,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  UNIQUE (provider, provider_account_id)
);

CREATE INDEX idx_oauth_accounts_user_id ON oauth_accounts(user_id);
```

### `verifications`

```sql
CREATE TABLE verifications (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  identifier TEXT NOT NULL,   -- e.g. "email-verification:user@example.com"
                              --      "password-reset:user@example.com"
  value      TEXT NOT NULL,   -- hashed token
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_verifications_identifier ON verifications(identifier);
```

### `events`

```sql
CREATE TABLE events (
  id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  organizer_id              UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name                      TEXT NOT NULL,
  description               TEXT,
  location                  TEXT NOT NULL,
  status                    TEXT NOT NULL DEFAULT 'draft',
                            -- 'draft', 'published', 'sold_out', 'cancelled', 'ended'
  starts_at                 TIMESTAMPTZ NOT NULL,
  ends_at                   TIMESTAMPTZ NOT NULL,
  capacity                  INTEGER,              -- nullable, NULL means no top-level cap
  cancellation_allowed      BOOLEAN NOT NULL DEFAULT TRUE,
  cancellation_hours_before INTEGER NOT NULL DEFAULT 0,
  max_tickets_per_purchase  INTEGER NOT NULL DEFAULT 10,
  material_changed_at       TIMESTAMPTZ,          -- NULL = no material change since publish
  created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT valid_dates CHECK (ends_at > starts_at),
  CONSTRAINT positive_capacity CHECK (capacity IS NULL OR capacity > 0),
  CONSTRAINT positive_max_tickets CHECK (max_tickets_per_purchase > 0),
  CONSTRAINT valid_cancellation_hours CHECK (cancellation_hours_before >= 0)
);

CREATE INDEX idx_events_organizer_id ON events(organizer_id);
CREATE INDEX idx_events_status ON events(status);
CREATE INDEX idx_events_starts_at ON events(starts_at);
```

### `ticket_tiers`

```sql
CREATE TABLE ticket_tiers (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  price      INTEGER NOT NULL,     -- in cents (USD)
  quantity   INTEGER NOT NULL,     -- total inventory
  remaining  INTEGER NOT NULL,     -- decremented on purchase
  status     TEXT NOT NULL DEFAULT 'available',
             -- 'available', 'sold_out'
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT positive_quantity CHECK (quantity > 0),
  CONSTRAINT valid_remaining CHECK (remaining >= 0 AND remaining <= quantity),
  CONSTRAINT positive_price CHECK (price >= 0)
);

CREATE INDEX idx_ticket_tiers_event_id ON ticket_tiers(event_id);
```

### `purchases`

```sql
CREATE TABLE purchases (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tier_id    UUID NOT NULL REFERENCES ticket_tiers(id) ON DELETE CASCADE,
  quantity   INTEGER NOT NULL,
  total      INTEGER NOT NULL,     -- price * quantity at time of purchase, in cents
  status     TEXT NOT NULL DEFAULT 'confirmed',
             -- 'confirmed', 'cancelled', 'refunded'
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT positive_quantity CHECK (quantity > 0),
  CONSTRAINT positive_total CHECK (total >= 0)
);

CREATE INDEX idx_purchases_user_id ON purchases(user_id);
CREATE INDEX idx_purchases_tier_id ON purchases(tier_id);
```

`total` is stored at purchase time — not computed on the fly. If the organizer ever changes the tier price, historical purchases still reflect what the attendee actually paid.

### `tickets`

One row per individual ticket. A single purchase of quantity 2 generates 2 ticket rows.

```sql
CREATE TABLE tickets (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  purchase_id   UUID NOT NULL REFERENCES purchases(id) ON DELETE CASCADE,
  tier_id       UUID NOT NULL REFERENCES ticket_tiers(id) ON DELETE CASCADE,
  qr_token      TEXT NOT NULL UNIQUE,  -- HMAC-SHA256 signed token
  status        TEXT NOT NULL DEFAULT 'unused',
                -- 'unused', 'used', 'cancelled'
  checked_in_at TIMESTAMPTZ,           -- nullable, set on check-in
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tickets_purchase_id ON tickets(purchase_id);
CREATE INDEX idx_tickets_qr_token ON tickets(qr_token);
```

`idx_tickets_qr_token` is the hot index — every check-in lookup hits it.

### `waitlist_entries`

```sql
CREATE TABLE waitlist_entries (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tier_id     UUID NOT NULL REFERENCES ticket_tiers(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'waiting',
              -- 'waiting', 'notified', 'purchased', 'expired'
  notified_at TIMESTAMPTZ,             -- when the user was notified of availability
  expires_at  TIMESTAMPTZ,             -- deadline to complete purchase after notification
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  UNIQUE (user_id, tier_id)            -- one entry per user per tier
);

CREATE INDEX idx_waitlist_entries_tier_id ON waitlist_entries(tier_id);
CREATE INDEX idx_waitlist_entries_status ON waitlist_entries(status);
CREATE INDEX idx_waitlist_entries_created_at ON waitlist_entries(created_at);
```

`UNIQUE (user_id, tier_id)` prevents the same person joining the waitlist for the same tier twice. `idx_waitlist_entries_created_at` supports ordering the queue by join time.

---

## Project Structure

```
gater/
├── cmd/
│   ├── server/
│   │   ├── main.go          -- entry point, wires everything together
│   │   ├── api.go           -- application struct, mount(), run()
│   │   ├── auth.go          -- register, login, logout, verifyEmail, resendVerification, forgotPassword, resetPassword, google, googleCallback
│   │   ├── users.go         -- getUser, becomeOrganizer
│   │   ├── health.go        -- checkHealth
│   │   ├── events.go        -- event handlers: createEvent, getEvent, updateEvent, deleteEvent, publishEvent, cancelEvent, getPublishedEvents, publicEvent
│   │   ├── tiers.go         -- createTier, listTiers, updateTier, deleteTier
│   │   ├── purchases.go     -- createPurchase, listPurchases, getPurchase, cancelPurchase
│   │   ├── waitlist.go      -- joinWaitlist, leaveWaitlist, getWaitlist
│   │   ├── check-in.go      -- stub
│   │   ├── analytics.go     -- stub
│   │   ├── middleware.go    -- authenticate (shared core), requireAuth, maybeAuth, requireOrganizer, requireEventOrganizer, injectLogging
│   │   └── docs.go          -- scalarDocs, openAPISpec (planned)
│   └── migrate/
│       └── main.go          -- goose migration runner
├── internal/
│   ├── auth/
│   │   ├── password.go      -- Argon2id hashing + comparison
│   │   ├── token.go         -- secure token generation + hashing
│   │   └── oauth.go         -- OAuth state generation helper
│   ├── cache/
│   │   └── redis.go         -- Redis client (not yet wired)
│   ├── config/
│   │   └── config.go        -- env vars loaded into Config struct, fail fast
│   ├── db/
│   │   └── db.go            -- pgx pool setup
│   ├── jsonutil/
│   │   └── json.go          -- Read, Write, WriteData, WriteError helpers
│   ├── mailer/
│   │   ├── mailer.go        -- Mailer interface + embedded email templates
│   │   ├── resend.go        -- Resend client implementation
│   │   └── templates/       -- verification.html, password-reset.html
│   ├── qr/
│   │   └── qr.go            -- HMAC-SHA256 token generation + verification
│   ├── store/
│   │   ├── store.go         -- Store struct with per-domain interfaces + New()
│   │   ├── users.go         -- UserStore + user queries
│   │   ├── sessions.go      -- SessionStore + session queries
│   │   ├── verifications.go -- VerificationStore + verification queries
│   │   ├── oauth.go         -- OAuthStore + oauth account queries
│   │   ├── events.go        -- EventsStore + event queries
│   │   ├── tiers.go         -- TiersStore + tier queries
│   │   ├── purchases.go     -- PurchaseStore + purchase queries (incl. the Create/Cancel transactions)
│   │   ├── tickets.go       -- TicketStore (planned)
│   │   └── waitlist.go      -- WaitlistStore + waitlist queries
│   └── validator/
│       └── validator.go     -- go-playground/validator wrapper
├── requests/                       -- Bruno API requests
├── .env
├── .env.example
├── .air.toml
├── .gitignore
├── docker-compose.yml               -- local dev: Postgres + Redis
├── Dockerfile
├── go.mod
├── justfile
└── README.md
```

### `application` Struct

```go
type application struct {
    config    *config.Config
    store     store.Store
    mailer    mailer.Mailer
    validator validator.Validator
    logger    *slog.Logger
}
```

Cache (`cache.Cache`) will be added when Redis is wired in.

### `justfile`

```just
alias d := dev

dev: db-up
    air

build:
    go build -o bin/server cmd/server

migrate:
    go run cmd/migrate/main.go

db-up:
    docker compose up -d

db-down:
    docker compose down

db-delete:
    docker compose down -v
```

### Go Dependencies

```
github.com/go-chi/chi/v5              -- router + middleware
github.com/go-chi/cors                -- CORS middleware
github.com/jackc/pgx/v5               -- Postgres driver
github.com/redis/go-redis/v9          -- Redis client
github.com/pressly/goose/v3           -- db migrations
github.com/lib/pq                      -- database/sql driver for goose
github.com/resend/resend-go/v3        -- email sending
github.com/go-playground/validator/v10 -- request validation
github.com/joho/godotenv              -- .env loading
github.com/google/uuid                -- UUID generation
golang.org/x/crypto                   -- Argon2id password hashing
golang.org/x/oauth2                   -- Google OAuth
github.com/hibiken/asynq              -- background job queue (planned)
```

**Dev tools (installed globally, not in go.mod):**

```
github.com/air-verse/air         -- live reloading
```

### `.air.toml`

```toml
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -o ./tmp/main ./cmd/server"
  entrypoint = ["./tmp/main"]
  watch_dir = "."
  include_ext = ["go"]
  exclude_dir = ["assets", "tmp", "vendor", "testdata"]

[log]
  time = true
```

### `docker-compose.yml`

```yaml
name: gater

services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_DB: gater
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    ports:
      - "5435:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres -d gater"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:8-alpine
    ports:
      - "6380:6379"
    volumes:
      - redis_data:/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  pgdata:
  redis_data:
```

---

## Phases

### Phase 1 — Project Scaffolding ✅

- Initialize Go module at repo root (`go mod init github.com/chiagoziem/gater`)
- Create full folder structure (`cmd/`, `internal/`, `migrations/`, `requests/`)
- Set up `.env.example` and `.gitignore`
- Set up `docker-compose.yml` for local Postgres + Redis
- Set up `justfile`
- Set up Air with `.air.toml`
- Set up `README.md`

### Phase 2 — Database & Migrations ✅

- Set up `pgx` pool in `internal/db/db.go`
- Set up `goose/v3` as the migration runner in `cmd/migrate/main.go` (embedded `//go:embed migrations/*.sql`)
- Write individual numbered migration files in `cmd/migrate/migrations/`:
  - `00001_create_trigger_function.sql` — `updated_at` trigger
  - `00002_create_users_table.sql`
  - `00003_create_sessions_table.sql`
  - `00004_create_oauth_accounts_table.sql`
  - `00005_create_verifications_table.sql`
  - `00006_create_events_table.sql`
  - `00007_create_ticket_tiers_table.sql`
  - `00008_create_purchases_table.sql`
  - `00009_create_tickets_table.sql`
  - `00010_create_waitlist_entries_table.sql`
- Verify migrations run cleanly against local Postgres

### Phase 3 — Config & Server Skeleton ✅

- Write `internal/config/config.go` — load all env vars, fail fast on missing required ones
- Wire up `cmd/server/main.go` — load config, init DB, init mailer, init validator, wire `application`, call `run()`
- Set up Chi router with middleware (`CleanPath`, `StripSlashes`, `RequestID`, `RealIP`, `Logger`, `Recoverer`, `CORS`, `Timeout`, `injectLogging`)
- Register all routes — health and auth handlers implemented, rest are stubs
- Implement `run()` with graceful shutdown (`SIGINT`/`SIGTERM`)
- Verify server starts and all routes respond

### Phase 4 — Store Layer ✅

- Define `Store` struct with per-domain interfaces in `internal/store/store.go`
- Implement store methods across `users.go`, `sessions.go`, `verifications.go`, `oauth.go`
- No handlers yet — just the DB layer verified against local Postgres
- **Implemented:** Users, Sessions, Verifications, OAuthAccounts
- **Planned:** Events, Tiers, Purchases, Tickets, Waitlist

### Phase 5 — Auth (Email + Password) ✅

- `internal/auth/password.go` — Argon2id hash + compare
- `internal/auth/token.go` — `GenerateToken()` + `HashToken()`
- `internal/mailer/mailer.go` — Resend client, Mailer interface, email templates
- Implement handlers: `register`, `login`, `logout`, `me`, `becomeOrganizer`
- Implement email verification: `verifyEmail`, `resendVerification`
- Implement password reset: `forgotPassword`, `resetPassword`
- Auth middleware in `cmd/server/middleware.go` — Bearer header first, `gater_auth_session` cookie fallback
- Rate limiting on resend-verification and forgot-password: 5 per hour, 1 min cooldown
- Session create retries up to 3× on hash collision

### Phase 6 — Auth (Google OAuth) ✅

- `internal/auth/oauth.go` — OAuth state generation helper
- Implement `GET /api/auth/google` and `GET /api/auth/google/callback`
- Handle new user and existing user cases in callback
- Link OAuth account to existing email/password account if same email
- OAuth state validated via signed cookie (`gater_oauth_state`, 10 min expiry)
- Existing unverified email/password users get auto-verified on first Google login

### Phase 7 — Events ✅

- Implement event handlers: `createEvent`, `listEvents` (`getPublishedEvents`), `getEvent`, `updateEvent`, `deleteEvent`
- Implement `publishEvent` and `cancelEvent` with state machine validation
- `requireOrganizer` middleware — checks user role is `organizer`
- `requireEventOrganizer` middleware — checks the authenticated user owns the event
- Update policy: material fields require `confirm_material_change`, cancelled/ended events frozen, draft-only delete
- Draft/cancelled events hidden from non-organizers (`publicEvent`, `maybeAuth`)

### Phase 8 — Ticket Tiers ✅

- Implement tier handlers: `createTier`, `listTiers`, `updateTier`, `deleteTier`
- Capacity validation on create/update — sum of tier quantities must not exceed event capacity
- Post-publish rules: name/price editable anytime, quantity `>= sold`, `sold_out` flips at tier and event level
- Delete locked to draft events only
- Publish gate: reject publish when the event has no tiers

### Phase 9 — Purchases + Tickets ✅

- `internal/qr/qr.go` — `GenerateToken()` + `VerifyToken()` using HMAC-SHA256
- Implement `createPurchase` with `SELECT FOR UPDATE` transaction
  - single tx: lock tier → authoritative checks → decrement inventory → insert purchase → batch-insert tickets (QR tokens signed by handler, DB values backfilled via `RETURNING`) → event `sold_out` flip
- Implement `getPurchase` (owner-scoped, 404 hides other users' purchases) and `listPurchases` (offset pagination — page/limit/total; a history archive reads better in pages than the events feed's cursor)
- Cancellation policy enforcement on `cancelPurchase`: started/allowed/window gates, cancelled-event purchases cancellable, inventory restoration, event `sold_out → published` flip
- Capacity floor on event updates: capacity can't drop below confirmed tickets sold
- Material-change 409 reports affected ticket holders (zero-buyer case gets its own message)
- Grace window: confirmed material changes stamp `material_changed_at`, letting buyers cancel up to 72h past the change (`Events.Update` preserves stamps via COALESCE)

### Phase 10 — Waitlist (entry management) ✅

- Implement `joinWaitlist` (visibility + ended gates, sold-out-only, no-tickets check, unique-entry 409), `leaveWaitlist` (hard delete via pair-scoped DELETE), `getWaitlist` (organizer FIFO view with buyer identity + offer timestamps)
- Waitlist entry validation — user must not already have a ticket for the tier
- Promotion-on-cancellation deferred to Phase 12: TODO hook sits in `PurchasesStore.Cancel`; strict rejoin upgrade ships with the expiry worker

### Phase 11 — Check-in

- Implement `checkIn` handler with QR token verification
- `SELECT FOR UPDATE` on ticket row to prevent duplicate check-ins
- Return descriptive errors for all invalid cases

### Phase 12 — Background Jobs (Asynq)

- Set up Asynq client and server in `internal/worker/`
- Implement `EndExpiredEvents` scheduled job — runs every 5 minutes
- Implement `ExpireWaitlistReservations` scheduled job — runs every 5 minutes
- Implement `NotifyWaitlistEntry` triggered job — enqueued on purchase cancellation
- Wire worker server startup alongside HTTP server in `main.go`

### Phase 13 — Analytics

- Implement `getEventAnalytics` — total purchases, check-in rate, revenue, tier breakdown
- Implement `getOrganizerEvents` — all organizer events across all statuses

### Phase 14 — API Documentation

- Decide on OpenAPI generation approach
- Generate or write spec
- Serve Scalar UI at `GET /api/docs`
- Serve raw spec at `GET /api/openapi.yaml`

### Phase 15 — Dockerization & Deployment

- Write `Dockerfile` — multi-stage build, lean final image
- Configure Dokploy — environment variables, Postgres + Redis services
- Set up Cloudflare for DNS
- Smoke test in production

```sql
BEGIN;

-- lock the tier row to prevent concurrent purchases
SELECT id, remaining, status
FROM ticket_tiers
WHERE id = $1
FOR UPDATE;

-- application checks:
-- remaining >= quantity requested
-- remaining > 0
-- max_tickets_per_purchase not exceeded

-- decrement remaining, auto-transition to sold_out if hits 0
UPDATE ticket_tiers
SET
  remaining  = remaining - $quantity,
  status     = CASE WHEN remaining - $quantity = 0 THEN 'sold_out' ELSE 'available' END,
  updated_at = NOW()
WHERE id = $1;

-- create purchase record
INSERT INTO purchases (user_id, tier_id, quantity, total)
VALUES ($user_id, $tier_id, $quantity, $price * $quantity)
RETURNING id;

-- create one ticket row per unit purchased
INSERT INTO tickets (purchase_id, tier_id, qr_token)
VALUES
  ($purchase_id, $tier_id, $token1),
  ($purchase_id, $tier_id, $token2);

-- if tier is now sold_out, check if all tiers for this event are sold_out
-- if yes, update event status to 'sold_out'

COMMIT;
```

`SELECT FOR UPDATE` locks the tier row for the duration of the transaction. Any concurrent purchase on the same tier waits until this transaction commits or rolls back. No overselling possible. The `CHECK (remaining >= 0)` constraint on `ticket_tiers` is a final DB-level safety net.

---

## QR Token Structure

Each ticket's `qr_token` is an HMAC-SHA256 signed payload:

```
payload   = ticketID + ":" + purchaseID + ":" + tierID
signature = HMAC-SHA256(payload, TICKET_SECRET)
token     = base64url(payload + "." + signature)
```

At check-in:

1. Decode the token from base64url
2. Split on `.` to get payload and signature
3. Recompute `HMAC-SHA256(payload, TICKET_SECRET)`
4. Compare with the token's signature — if mismatch, reject
5. Extract `ticketID` from payload
6. Look up ticket in DB, verify status is `unused`
7. Lock ticket row (`SELECT FOR UPDATE`), mark as `used`, set `checked_in_at`

Lives in `internal/qr/qr.go`:

```go
func GenerateToken(ticketID, purchaseID, tierID, secret string) (string, error)
func VerifyToken(token, secret string) (ticketID string, err error)
```

---

## Background Jobs (Asynq)

### Scheduled (recurring)

**`EndExpiredEvents`** — runs every 5 minutes:

```sql
UPDATE events
SET status = 'ended', updated_at = NOW()
WHERE status IN ('published', 'sold_out')
  AND ends_at < NOW();
```

**`ExpireWaitlistReservations`** — runs every 5 minutes:

```sql
UPDATE waitlist_entries
SET status = 'expired', updated_at = NOW()
WHERE status = 'notified'
  AND expires_at < NOW()
RETURNING tier_id;
-- for each returned tier_id, enqueue NotifyWaitlistEntry
```

### Triggered

**`NotifyWaitlistEntry`** — enqueued when a purchase is cancelled:

1. Find the next `waiting` entry for the tier ordered by `created_at`
2. Set status to `notified`, set `notified_at = NOW()`, `expires_at = NOW() + 24h`
3. Send email to the user via Resend

---

## Capacity Validation

When creating or updating a tier, validate against event capacity:

```go
// sum existing tier quantities for this event
existingTotal, err := app.store.Tiers.SumQuantityByEvent(ctx, eventID)

if event.Capacity != nil && existingTotal + newQuantity > *event.Capacity {
    writeError(w, http.StatusBadRequest,
        fmt.Sprintf("total tier capacity (%d) would exceed event capacity (%d)",
            existingTotal + newQuantity, *event.Capacity))
    return
}
```

---

## Backlog

Ideas we deliberately deferred. Not on the roadmap, but not forgotten.

### Custom tier ordering

Currently tiers are listed in `created_at` order. To let organizers control order (e.g. pin VIP/first-access tiers first):

- Add a `sort_order` column to `ticket_tiers` (new migration, `INTEGER NOT NULL DEFAULT 0`)
- Order listings by `sort_order, created_at`
- Expose `sort_order` as a PATCH-able field on tier updates (or a dedicated reorder endpoint)

Explicit position is preferred over inferring order from `price`, since multiple tiers can share a price.

```

```
