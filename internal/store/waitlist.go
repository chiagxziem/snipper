package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WaitlistStore struct {
	pool *pgxpool.Pool
}

type WaitlistEntry struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	TierID     uuid.UUID  `json:"tier_id"`
	Status     string     `json:"status"`
	NotifiedAt *time.Time `json:"notified_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (s *WaitlistStore) Create(ctx context.Context, entry *WaitlistEntry) error {
	query := `
		INSERT INTO waitlist_entries (user_id, tier_id)
		VALUES ($1, $2)
		RETURNING id, user_id, tier_id, status, notified_at, expires_at,
		created_at, updated_at
	`

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	err := s.pool.QueryRow(
		ctx, query, entry.UserID, entry.TierID,
	).Scan(
		&entry.ID, &entry.UserID, &entry.TierID, &entry.Status,
		&entry.NotifiedAt, &entry.ExpiresAt, &entry.CreatedAt, &entry.UpdatedAt,
	)
	if err != nil {
		// postgres unique_violation: this user already has a row for this
		// tier in ANY state (waiting/notified/expired/purchased)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("store: create waitlist entry: %w", ErrConflict)
		}
		return fmt.Errorf("store: create waitlist entry: %w", err)
	}

	return nil
}
