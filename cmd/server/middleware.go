package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/goziemsunday/gater/internal/auth"
	"github.com/goziemsunday/gater/internal/jsonutil"
	"github.com/goziemsunday/gater/internal/store"
)

// auth failure sentinels
var (
	errMalformedHeader = errors.New("malformed authorization header")
	errMissingToken    = errors.New("missing authorization token")
	errInvalidSession  = errors.New("invalid session")
)

// authenticate resolves the session and the user that the request belongs
// to (or nil for both if the req belongs to a guest), and sentinel errors
// if an error occurs
func (a *application) authenticate(r *http.Request) (*store.Session, *store.User, error) {
	logger := loggerFromCtx(r.Context())

	authHeader := r.Header.Get("Authorization")

	var token string

	// check authorization header first
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return nil, nil, errMalformedHeader
		}
		token = parts[1]
	} else {
		// if no auth header, check for browser-sent cookie
		authCookie, err := r.Cookie("gater_auth_session")
		if err != nil {
			return nil, nil, errMissingToken
		}
		token = authCookie.Value
	}

	if token == "" {
		return nil, nil, errMissingToken
	}

	hashedToken := auth.HashToken(token)

	session, err := a.store.Sessions.Get(r.Context(), hashedToken)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, errInvalidSession
		}
		logger.Error("failed to get session", "error", err, "hashed_token", hashedToken[:8]+"...")
		return nil, nil, errInvalidSession
	}

	user, err := a.store.Users.GetByID(r.Context(), session.UserID.String())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, errInvalidSession
		}
		logger.Error("failed to get user", "error", err, "session_id", session.ID)
		return nil, nil, errInvalidSession
	}

	return session, user, nil
}

func (a *application) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, user, err := a.authenticate(r)
		if err != nil {
			switch {
			case errors.Is(err, errMalformedHeader):
				jsonutil.WriteError(w, http.StatusUnauthorized, "malformed authorization header")
			case errors.Is(err, errMissingToken):
				jsonutil.WriteError(w, http.StatusUnauthorized, "missing authorization token")
			default:
				jsonutil.WriteError(w, http.StatusUnauthorized, "unauthorized")
			}
			return
		}

		ctx := context.WithValue(r.Context(), userCtx, user)
		ctx = context.WithValue(ctx, sessionCtx, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// maybeAuth tries to authenticate the req just like requireAuth but never rejects it
func (a *application) maybeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, user, err := a.authenticate(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userCtx, user)
		ctx = context.WithValue(ctx, sessionCtx, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireOrganizer must be used after requireAuth so the user is already in the context
func (a *application) requireOrganizer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(userCtx).(*store.User)
		if !ok {
			jsonutil.WriteError(w, http.StatusInternalServerError, "user not found in context")
			return
		}

		if user.Role != store.RoleOrganizer {
			jsonutil.WriteError(w, http.StatusForbidden, "organizer role required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *application) injectLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		logger := a.logger.With("request_id", reqID)
		ctx := context.WithValue(r.Context(), loggerCtx, logger)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loggerFromCtx(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(loggerCtx).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return logger
}
