# Gater

Event ticketing API. Go 1.26, PostgreSQL, Redis.

## Quick start

```sh
just dev       # docker compose up -d + air (hot reload)
just d         # alias for dev
just migrate   # runs goose migrations (go run cmd/migrate/main.go)
just db-up     # docker compose up -d (PG:5435, Redis:6380)
just db-down   # docker compose down
just db-delete # docker compose down -v
go build -o bin/server ./cmd/server
```

No tests, no linter, no formatter config.

## Architecture

`cmd/server/` — `package main`, HTTP handlers, chi routing, middleware.  
`internal/` — `config/` (godotenv), `db/` (pgxpool), `store/` (raw SQL via pgx, 5s per-query timeout; `users`, `sessions`, `verifications`, `oauth`, `events`, `tiers`), `auth/` (argon2id, SHA-256 tokens), `jsonutil/`, `validator/` (go-playground), `mailer/` (Resend).  
`cmd/migrate/` — goose runner with embedded SQL.  
`internal/cache/redis.go` exists but is **not wired into the app**.

Handlers manually wired into `application` struct in `main.go` — no DI framework.

## Key conventions

- **Auth:** `authenticate` is the single auth core (Bearer header first, `gater_auth_session` cookie fallback). Wrappers: `requireAuth` (rejects with 401), `maybeAuth` (never rejects — runs handlers as guest when unauthenticated). Token = 32-byte random → hex → SHA-256 → store hash. Session create retries up to 3× on hash collision.
- **Middleware order:** `requireAuth` → `requireOrganizer` (role check) → `requireEventOrganizer` (ownership check, loads event into `eventCtx`).
- **Public-event visibility:** draft/cancelled events return 404 to non-organizers (no existence leak). `publicEvent` helper enforces this on public routes.
- **Event updates:** draft events are fully editable. Published/sold_out: material fields (dates, location, cancellation policy) need `confirm_material_change: true`, capacity can't drop below tickets sold; cancelled/ended events are frozen (409). Name/description/max_tickets editable anytime.
- **Cookie** `gater_auth_session` set on login (HttpOnly, Lax, 30d, Secure only in production, `SameSite=Lax`). CORS `AllowCredentials: true` lets browsers send it cross-origin.
- **JSON response:** Success `{"data": ...}` via `WriteData`, errors `{"errors": [...]}` via `WriteError` (single msg) / `WriteErrors` (validation). Exception: health check uses bare `Write` → `{"status":"OK"}`.
- **Password** `json:"-"` — never serialized to JSON. `internal/store/` uses raw SQL; `PurchasesStore.Create` is the only transaction so far (`pool.Begin` + deferred rollback inside one store method — handlers never see `pgx.Tx`).
- **Background email** uses `context.Background()`, errors only logged.
- **Tier payloads:** `price`/`quantity` are pointers on create (omitted ≠ 0); update payloads are pointer-based so omitted fields stay unchanged.
- **Tier updates:** `name`/`price` editable anytime (purchase totals are stored per purchase); `quantity` must stay `>= sold` (422) with `remaining` recomputed; capacity check nets out the tier's old quantity; tier `sold_out → available` and event `sold_out → published` flips on quantity increase; delete is draft-only; cancelled/ended events freeze all tier edits (409).

## Routes (`/api`)

```
GET  /api/health
POST /api/auth/register, login, verify-email, resend-verification, forgot-password, reset-password
GET  /api/auth/google, google/callback
POST /api/auth/logout, become-organizer  (protected)
GET  /api/auth/me                        (protected)

GET    /api/events                      (public, cursor-paginated published only)
GET    /api/events/{id}                 (public; drafts private via publicEvent)
POST   /api/events                      (organizer)
PATCH  /api/events/{id}                 (organizer, ownership)
DELETE /api/events/{id}                 (organizer, draft only)
POST   /api/events/{id}/publish         (organizer, draft only)
POST   /api/events/{id}/cancel          (organizer, draft/published/sold_out)
GET    /api/events/{id}/tiers           (public; drafts private)
POST   /api/events/{id}/tiers           (organizer, draft only, capacity-checked)
PATCH  /api/events/{id}/tiers/{tierId}  (organizer, ownership; quantity >= sold, sold_out flips)
DELETE /api/events/{id}/tiers/{tierId}  (organizer, draft only)
POST   /api/purchases                   (protected; tx: tier lock → checks → decrement → tickets)
```

## Implemented vs stubs

| File                                                                                  | Status      |
| ------------------------------------------------------------------------------------- | ----------- |
| `auth.go`, `users.go`, `health.go`, `middleware.go`, `events.go`, `tiers.go`         | Implemented |
| `purchases.go`                                                                        | Partial: createPurchase done |
| `check-in.go`, `waitlist.go`, `analytics.go`                                           | Empty stubs |

## Requests

`requests/` directory contains a [Bruno](https://docs.usebruno.com) API collection (`opencollection.yml`) with request examples for auth endpoints.

## Known quirks

- `internal/cache/redis.go` imports redis client but nothing in the app uses it.
