package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/goziemsunday/gater/internal/cursor"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrConflict                  = errors.New("resource already exists")
	ErrNotFound                  = errors.New("resource not found")
	ErrEventStarted              = errors.New("event has already started")
	ErrAlreadyCancelled          = errors.New("purchase has already been cancelled")
	ErrEventNotPublished         = errors.New("event is not open for ticket sales")
	ErrInsufficientRemaining     = errors.New("not enough tickets remaining")
	ErrExceedsMaxPerPurchase     = errors.New("quantity exceeds the maximum tickets per purchase")
	ErrCancellationNotAllowed    = errors.New("event does not allow cancellations")
	ErrOutsideCancellationWindow = errors.New("cancellation window has closed")
)

var queryTimeoutDuration = time.Second * 5

// materialChangeGracePeriod is how long buyers keep an extended right to
// cancel after an organizer lands a confirmed material change on their
// event. Capped at the event start.
const materialChangeGracePeriod = time.Hour * 72

const (
	RoleOrganizer string = "organizer"
	RoleAttendee  string = "attendee"
)

type Store struct {
	Users interface {
		Create(ctx context.Context, user *User) error
		GetByID(ctx context.Context, id string) (*User, error)
		GetByEmail(ctx context.Context, email string) (*User, error)
		MarkVerified(ctx context.Context, email string) error
		ResetPassword(ctx context.Context, email, hashedPassword string) error
		BecomeOrganizer(ctx context.Context, userID string) (*User, error)
	}
	Sessions interface {
		Create(ctx context.Context, session *Session) error
		Get(ctx context.Context, hashedToken string) (*Session, error)
		Delete(ctx context.Context, sessionID uuid.UUID) error
		DeleteAll(ctx context.Context, userID uuid.UUID) error
	}
	Verifications interface {
		Create(ctx context.Context, params CreateVerificationParams) error
		Get(ctx context.Context, hashedToken string) (*Verifications, error)
		GetLatest(ctx context.Context, identifier string) (*Verifications, error)
		CountSince(ctx context.Context, identifier string, since time.Duration) (int, error)
		Delete(ctx context.Context, ID string) error
		DeleteByIdentifier(ctx context.Context, identifier string) error
	}
	OAuthAccounts interface {
		GetByProviderAndAccountID(ctx context.Context, provider, accountID string) (*OAuthAccount, error)
		Create(ctx context.Context, account *OAuthAccount) error
	}
	Events interface {
		GetAllPublished(ctx context.Context, cursor *cursor.Cursor, limit int) ([]*Event, error)
		GetByID(ctx context.Context, id string) (*Event, error)
		Create(ctx context.Context, event *Event) error
		Update(ctx context.Context, event *Event) error
		Delete(ctx context.Context, id string) error
		Publish(ctx context.Context, id string) (*Event, error)
		Cancel(ctx context.Context, id string) (*Event, error)
		SetStatus(ctx context.Context, id, from, to string) error
	}
	Tiers interface {
		Create(ctx context.Context, tier *Tier) error
		GetByID(ctx context.Context, id string) (*Tier, error)
		Update(ctx context.Context, tier *Tier) error
		Delete(ctx context.Context, id, eventID string) error
		CountByEvent(ctx context.Context, eventID string) (int, error)
		ListByEvent(ctx context.Context, eventID string) ([]*Tier, error)
		SumQuantityByEvent(ctx context.Context, eventID string) (int, error)
	}
	Purchases interface {
		Create(ctx context.Context, purchase *Purchase, tickets []Ticket) error
		GetByID(ctx context.Context, id, userID string) (*Purchase, error)
		Cancel(ctx context.Context, id, userID string) (*Purchase, error)
		ListTicketsByPurchase(ctx context.Context, purchaseID string) ([]Ticket, error)
		ListByUser(ctx context.Context, userID string, limit, offset int) ([]PurchaseSummary, error)
		CountByUser(ctx context.Context, userID string) (int, error)
		SumConfirmedQuantityByEvent(ctx context.Context, eventID string) (int, error)
		HasConfirmedPurchase(ctx context.Context, userID, tierID string) (bool, error)
	}
	Waitlist interface {
		Create(ctx context.Context, entry *WaitlistEntry) error
		DeleteByUserAndTier(ctx context.Context, userID, tierID string) error
	}
}

func New(pool *pgxpool.Pool) Store {
	return Store{
		Users:         &UserStore{pool},
		Sessions:      &SessionStore{pool},
		Verifications: &VerificationStore{pool},
		OAuthAccounts: &OAuthStore{pool},
		Events:        &EventsStore{pool},
		Tiers:         &TiersStore{pool},
		Purchases:     &PurchasesStore{pool},
		Waitlist:      &WaitlistStore{pool},
	}
}
