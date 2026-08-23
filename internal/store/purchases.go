package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PurchasesStore struct {
	pool *pgxpool.Pool
}

type Purchase struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TierID    uuid.UUID `json:"tier_id"`
	Quantity  int       `json:"quantity"`
	Total     int       `json:"total"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Ticket struct {
	ID          uuid.UUID  `json:"id"`
	PurchaseID  uuid.UUID  `json:"purchase_id"`
	TierID      uuid.UUID  `json:"tier_id"`
	QRToken     string     `json:"qr_token"`
	Status      string     `json:"status"`
	CheckedInAt *time.Time `json:"checked_in_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// tickets are excluded entirely from the purchase summary, because
// QR tokens are bulky and belong behind /purchases/{id}.
type PurchaseSummary struct {
	ID       uuid.UUID `json:"id"`
	Quantity int       `json:"quantity"`
	Total    int       `json:"total"`
	Status   string    `json:"status"`
	Event    struct {
		ID       uuid.UUID `json:"id"`
		Name     string    `json:"name"`
		StartsAt time.Time `json:"starts_at"`
	} `json:"event"`
	Tier struct {
		ID    uuid.UUID `json:"id"`
		Name  string    `json:"name"`
		Price int       `json:"price"`
	} `json:"tier"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *PurchasesStore) ListByUser(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]PurchaseSummary, error) {
	query := `
		SELECT p.id, p.quantity, p.total, p.status, p.created_at,
		e.id, e.name, e.starts_at,
		t.id, t.name, t.price
		FROM purchases p
		JOIN ticket_tiers t ON t.id = p.tier_id
		JOIN events e ON e.id = t.event_id
		WHERE p.user_id = $1
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $2 OFFSET $3
	`

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	rows, err := s.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: list purchases by user: %w", err)
	}
	defer rows.Close()

	// non-nil so an empty history marshals as [] not null
	summaries := make([]PurchaseSummary, 0)

	for rows.Next() {
		var summary PurchaseSummary
		err := rows.Scan(
			&summary.ID, &summary.Quantity, &summary.Total, &summary.Status, &summary.CreatedAt,
			&summary.Event.ID, &summary.Event.Name, &summary.Event.StartsAt,
			&summary.Tier.ID, &summary.Tier.Name, &summary.Tier.Price,
		)
		if err != nil {
			return nil, fmt.Errorf("store: list purchases by user: %w", err)
		}
		summaries = append(summaries, summary)
	}
	// get errors the iteration itself hit
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list purchases by user: %w", err)
	}

	return summaries, nil
}

func (s *PurchasesStore) CountByUser(ctx context.Context, userID string) (int, error) {
	// the DB index `idx_purchases_user_id` makes this query cheap
	query := `
		SELECT COUNT(*)
		FROM purchases
		WHERE user_id = $1
	`

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	var count int
	err := s.pool.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count purchases by user: %w", err)
	}

	return count, nil
}

func (s *PurchasesStore) SumConfirmedQuantityByEvent(ctx context.Context, eventID string) (int, error) {
	// COALESCE ensures 0 is returned if no sales has been made (SUM over zero rows is NULL)
	query := `
		SELECT COALESCE(SUM(p.quantity), 0)
		FROM purchases p
		JOIN ticket_tiers t ON t.id = p.tier_id
		WHERE t.event_id = $1 AND p.status = 'confirmed'
	`

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	var sum int
	err := s.pool.QueryRow(ctx, query, eventID).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("store: sum confirmed quantity by event: %w", err)
	}

	return sum, nil
}

func (s *PurchasesStore) GetByID(ctx context.Context, id, userID string) (*Purchase, error) {
	query := `
		SELECT id, user_id, tier_id, quantity, total, status,
		created_at, updated_at
		FROM purchases
		WHERE id = $1 AND user_id = $2
	`

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	purchase := &Purchase{}
	err := s.pool.QueryRow(ctx, query, id, userID).Scan(
		&purchase.ID, &purchase.UserID, &purchase.TierID, &purchase.Quantity,
		&purchase.Total, &purchase.Status, &purchase.CreatedAt, &purchase.UpdatedAt,
	)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("store: get purchase by id: %w", ErrNotFound)
		default:
			return nil, fmt.Errorf("store: get purchase by id: %w", err)
		}
	}

	return purchase, nil
}

func (s *PurchasesStore) ListTicketsByPurchase(ctx context.Context, purchaseID string) ([]Ticket, error) {
	query := `
		SELECT id, purchase_id, tier_id, qr_token, status,
		checked_in_at, created_at, updated_at
		FROM tickets
		WHERE purchase_id = $1
		ORDER BY created_at ASC
	`

	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	rows, err := s.pool.Query(ctx, query, purchaseID)
	if err != nil {
		return nil, fmt.Errorf("store: list tickets by purchase: %w", err)
	}
	defer rows.Close()

	// create a slice of Ticket from the rows gotten from the DB query
	tickets, err := pgx.CollectRows(rows, pgx.RowTo[Ticket])
	if err != nil {
		return nil, fmt.Errorf("store: collect tickets by purchase: %w", err)
	}

	return tickets, nil
}

func (s *PurchasesStore) Create(
	ctx context.Context,
	purchase *Purchase,
	tickets []Ticket,
) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeoutDuration)
	defer cancel()

	// init tx
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: create purchase: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// lock the tier row; concurrent buyers queue until COMMIT
	const lockTier = `
		SELECT price, remaining, status, event_id
		FROM ticket_tiers
		WHERE id = $1
		FOR UPDATE
	`
	var price, remaining int
	var tierStatus string
	var eventID uuid.UUID
	err = tx.QueryRow(ctx, lockTier, purchase.TierID).Scan(
		&price, &remaining, &tierStatus, &eventID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("store: create purchase: %w", ErrNotFound)
		}
		return fmt.Errorf("store: create purchase: lock tier: %w", err)
	}

	// load the event's sale rules
	const loadEventRules = `
		SELECT status, max_tickets_per_purchase
		FROM events
		WHERE id = $1
	`
	var eventStatus string
	var maxTicketsPerPurchase int
	err = tx.QueryRow(ctx, loadEventRules, eventID).Scan(&eventStatus, &maxTicketsPerPurchase)
	// if tier already exists then the event it belongs to definitely exists
	// so no pgx.ErrNoRows error to check here
	if err != nil {
		return fmt.Errorf("store: create purchase: load event rules: %w", err)
	}

	switch {
	case eventStatus != "published":
		return fmt.Errorf("store: create purchase: %w", ErrEventNotPublished)
	case purchase.Quantity > maxTicketsPerPurchase:
		return fmt.Errorf("store: create purchase: %w", ErrExceedsMaxPerPurchase)
	case tierStatus != "available" || remaining < purchase.Quantity:
		return fmt.Errorf("store: create purchase: %w", ErrInsufficientRemaining)
	}

	// take the inventory
	const decrementTier = `
		UPDATE ticket_tiers
		SET remaining = remaining - $2,
				status = CASE WHEN remaining - $2 = 0 THEN 'sold_out' ELSE 'available' END
		WHERE id = $1
	`
	if _, err := tx.Exec(ctx, decrementTier, purchase.TierID, purchase.Quantity); err != nil {
		return fmt.Errorf("store: create purchase: decrement tier: %w", err)
	}

	// record the purchase
	purchase.Total = price * purchase.Quantity

	const insertPurchase = `
		INSERT INTO purchases (id, user_id, tier_id, quantity, total)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING status, created_at, updated_at
	`
	// the purchase ID is supplied by the handler (QR tokens embed it)
	err = tx.QueryRow(
		ctx, insertPurchase, purchase.ID, purchase.UserID,
		purchase.TierID, purchase.Quantity, purchase.Total,
	).Scan(&purchase.Status, &purchase.CreatedAt, &purchase.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: create purchase: insert purchase: %w", err)
	}

	// mint one ticket per unit
	// pgx.Batch queues N inserts and ships them in ONE network round trip
	// qr_token was signed by the handler; status defaults to 'unused'
	batch := &pgx.Batch{}
	for i := range tickets {
		batch.Queue(`
			INSERT INTO tickets (id, purchase_id, tier_id, qr_token)
			VALUES ($1, $2, $3, $4)
			RETURNING status, created_at, updated_at
		`, tickets[i].ID, purchase.ID, purchase.TierID, tickets[i].QRToken).
			QueryRow(func(row pgx.Row) error {
				err := row.Scan(&tickets[i].Status, &tickets[i].CreatedAt, &tickets[i].UpdatedAt)
				if err != nil {
					return fmt.Errorf("scan ticket %d: %w", i, err)
				}
				return nil
			})
	}

	// Close completes the batch
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("store: create purchase: insert tickets: %w", err)
	}

	// check if there's any stock left for the event
	var anyAvailable bool
	const anyStockLeft = `
		SELECT EXISTS (
			SELECT 1 FROM ticket_tiers WHERE event_id = $1 AND remaining > 0
		)
	`
	if err := tx.QueryRow(ctx, anyStockLeft, eventID).Scan(&anyAvailable); err != nil {
		return fmt.Errorf("store: create purchase: check stock: %w", err)
	}
	// if there's no available tier, flip from published to sold_out
	if !anyAvailable {
		const sellOutEvent = `
			UPDATE events SET status = 'sold_out'
			WHERE id = $1 AND status = 'published'
		`
		if _, err := tx.Exec(ctx, sellOutEvent, eventID); err != nil {
			return fmt.Errorf("store: create purchase: flip event: %w", err)
		}
	}

	// commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: create purchase: commit: %w", err)
	}

	return nil
}
