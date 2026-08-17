package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TiersStore struct {
	pool *pgxpool.Pool
}

type Tier struct {
	ID        uuid.UUID `json:"id"`
	EventID   uuid.UUID `json:"event_id"`
	Name      string    `json:"name"`
	Price     int       `json:"price"`
	Quantity  int       `json:"quantity"`
	Remaining int       `json:"remaining"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *TiersStore) Create(ctx context.Context, tier *Tier) error {
	query := `
    INSERT INTO ticket_tiers (event_id, name, price, quantity, remaining)
    VALUES ($1, $2, $3, $4, $5)
    RETURNING id, event_id, name, price, quantity, remaining, status,
    created_at, updated_at
  `

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	err := s.pool.QueryRow(
		ctx, query, tier.EventID, tier.Name, tier.Price, tier.Quantity, tier.Remaining,
	).Scan(
		&tier.ID, &tier.EventID, &tier.Name, &tier.Price, &tier.Quantity,
		&tier.Remaining, &tier.Status, &tier.CreatedAt, &tier.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: create tier: %w", err)
	}

	return nil
}

func (s *TiersStore) SumQuantityByEvent(ctx context.Context, eventID string) (int, error) {
	query := `
    SELECT COALESCE(SUM(quantity), 0)
    FROM ticket_tiers
    WHERE event_id = $1
  `

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	var sum int
	err := s.pool.QueryRow(ctx, query, eventID).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("store: sum tier quantities by event: %w", err)
	}

	return sum, nil
}
