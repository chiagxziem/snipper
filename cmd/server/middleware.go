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

func (a *application) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := loggerFromCtx(r.Context())

		authHeader := r.Header.Get("Authorization")

		var token string

		// check authorization header first
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				jsonutil.WriteError(w, http.StatusUnauthorized, "malformed authorization header")
				return
			}
			token = parts[1]
		} else {
			// if no auth header, check for browser-sent cookie
			authCookie, err := r.Cookie("gater_auth_session")
			if err != nil {
				jsonutil.WriteError(w, http.StatusUnauthorized, "missing authorization token")
				return
			}
			token = authCookie.Value
		}

		if token == "" {
			jsonutil.WriteError(w, http.StatusUnauthorized, "missing authorization token")
			return
		}

		hashedToken := auth.HashToken(token)

		session, err := a.store.Sessions.Get(r.Context(), hashedToken)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound):
				jsonutil.WriteError(w, http.StatusUnauthorized, "unauthorized")
			default:
				logger.Error("failed to get session", "error", err, "hashed_token", hashedToken[:8]+"...")
				jsonutil.WriteError(w, http.StatusUnauthorized, "unauthorized")
			}
			return
		}

		user, err := a.store.Users.GetByID(r.Context(), session.UserID.String())
		if err != nil {
			switch {
			case errors.Is(err, store.ErrNotFound):
				jsonutil.WriteError(w, http.StatusUnauthorized, "unauthorized")
			default:
				logger.Error("failed to get user", "error", err, "session_id", session.ID)
				jsonutil.WriteError(w, http.StatusUnauthorized, "unauthorized")
			}
			return
		}

		ctx := context.WithValue(r.Context(), userCtx, user)
		ctx = context.WithValue(ctx, sessionCtx, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *application) requireOrganizer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// must be used after requireAuth so the user is already in the context
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
