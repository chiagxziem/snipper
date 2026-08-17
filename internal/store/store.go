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
	ErrConflict          = errors.New("resource already exists")
	ErrNotFound          = errors.New("resource not found")
	queryTimeoutDuration = time.Second * 5
)

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
		Create(ctx context.Context, event *Event) error
	}
}

func New(pool *pgxpool.Pool) Store {
	return Store{
		Users:         &UserStore{pool},
		Sessions:      &SessionStore{pool},
		Verifications: &VerificationStore{pool},
		OAuthAccounts: &OAuthStore{pool},
		Events:        &EventsStore{pool},
	}
}
