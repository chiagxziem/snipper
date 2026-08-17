package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/goziemsunday/gater/internal/cursor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventsStore struct {
	pool *pgxpool.Pool
}

type Event struct {
	ID                      uuid.UUID `json:"id"`
	OrganizerID             uuid.UUID `json:"organizer_id"`
	Name                    string    `json:"name"`
	Description             *string   `json:"description"`
	Location                string    `json:"location"`
	Status                  string    `json:"status"`
	StartsAt                time.Time `json:"starts_at"`
	EndsAt                  time.Time `json:"ends_at"`
	Capacity                *int      `json:"capacity"`
	CancellationAllowed     bool      `json:"cancellation_allowed"`
	CancellationHoursBefore int       `json:"cancellation_hours_before"`
	MaxTicketsPerPurchase   int       `json:"max_tickets_per_purchase"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func (s *EventsStore) Create(ctx context.Context, event *Event) error {
	query := `
    INSERT INTO events (
      organizer_id, name, description, location, starts_at, ends_at,
      capacity, cancellation_allowed, cancellation_hours_before,
      max_tickets_per_purchase
    )
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
    RETURNING id, organizer_id, name, description, location, status, starts_at,
    ends_at, capacity, cancellation_allowed, cancellation_hours_before,
    max_tickets_per_purchase, created_at, updated_at
  `

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	err := s.pool.QueryRow(
		ctx, query, event.OrganizerID, event.Name, event.Description, event.Location,
		event.StartsAt, event.EndsAt, event.Capacity, event.CancellationAllowed,
		event.CancellationHoursBefore, event.MaxTicketsPerPurchase,
	).Scan(
		&event.ID, &event.OrganizerID, &event.Name, &event.Description,
		&event.Location, &event.Status, &event.StartsAt, &event.EndsAt,
		&event.Capacity, &event.CancellationAllowed, &event.CancellationHoursBefore,
		&event.MaxTicketsPerPurchase, &event.CreatedAt, &event.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: create event: %w", err)
	}

	return nil
}

func (s EventsStore) GetPublished(
	ctx context.Context,
	cursor *cursor.Cursor,
	limit int,
) ([]*Event, error) {
	query := `
    SELECT id, organizer_id, name, description, location, status, starts_at,
    ends_at, capacity, cancellation_allowed, cancellation_hours_before,
    max_tickets_per_purchase, created_at, updated_at
    FROM events
    WHERE status = 'published'
  `

	var args []any
	if cursor != nil {
		query += `AND (starts_at, id) > ($1, $2)`
		args = append(args, cursor.Timestamp, cursor.ID)
	}

	query += `ORDER BY starts_at ASC, id ASC LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, limit)

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: get published events: %w", err)
	}
	defer rows.Close()

	events, err := pgx.CollectRows(rows, pgx.RowToAddrOf[Event])
	if err != nil {
		return nil, fmt.Errorf("store: collect published events: %w", err)
	}

	return events, nil
}
