package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/goziemsunday/gater/internal/config"
	"github.com/goziemsunday/gater/internal/jsonutil"
	"github.com/goziemsunday/gater/internal/mailer"
	"github.com/goziemsunday/gater/internal/store"
	"github.com/goziemsunday/gater/internal/validator"
)

type application struct {
	config    *config.Config
	store     store.Store
	mailer    mailer.Mailer
	validator validator.Validator
	logger    *slog.Logger
}

type contextKey string

const (
	userCtx    contextKey = "user"
	sessionCtx contextKey = "session"
	loggerCtx  contextKey = "logger"
)

func (a *application) mount() http.Handler {
	r := chi.NewRouter()

	// global middleware
	r.Use(middleware.CleanPath)
	r.Use(middleware.StripSlashes)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{a.config.CORSAllowedOrigin},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders: []string{"Link"},

		// Allow browsers to auto send cookies cross-origin.
		// AllowedOrigins must be an explicit origin for this to work.
		AllowCredentials: true,

		// Maximum value not ignored by any of major browsers
		MaxAge: 300,
	}))

	// Set a timeout value on the request context (ctx), that will signal
	// through ctx.Done() that the request has timed out and further
	// processing should be stopped.
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(a.injectLogging)

	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonutil.WriteError(w, http.StatusNotFound, "route not found")
	}))
	r.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonutil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	}))

	// api routes
	r.Route("/api", func(r chi.Router) {
		// health
		r.Get("/health", a.checkHealth)

		// auth
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", a.registerUser)
			r.Post("/login", a.loginUser)
			r.Post("/verify-email", a.verifyEmail)
			r.Post("/resend-verification", a.resendVerificationEmail)
			r.Post("/forgot-password", a.forgotPassword)
			r.Post("/reset-password", a.resetPassword)
			r.Get("/google", a.google)
			r.Get("/google/callback", a.googleCallback)

			// protected auth routes
			r.Group(func(r chi.Router) {
				r.Use(a.requireAuth)

				r.Get("/me", a.getUser)
				r.Post("/become-organizer", a.becomeOrganizer)
				r.Post("/logout", a.logoutUser)
			})
		})

		// events
		r.Route("/events", func(r chi.Router) {
			r.Get("/", a.getPublishedEvents)

			// `Get getEvent` is public, but it identifies the requester
			// inline so drafts stay private to their organizer
			r.Get("/{id}", a.getEvent)

			// protected events routes
			r.Group(func(r chi.Router) {
				r.Use(a.requireAuth)
				r.Use(a.requireOrganizer)

				r.Post("/", a.createEvent)
			})
		})
	})

	return r
}

func (a *application) run(mux http.Handler) error {
	srv := &http.Server{
		Addr:         ":" + a.config.Port,
		Handler:      mux,
		WriteTimeout: time.Second * 30,
		ReadTimeout:  time.Second * 10,
		IdleTimeout:  time.Minute,
	}

	shutdown := make(chan error)

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		s := <-quit

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		a.logger.Info("signal caught", "signal", s.String())

		shutdown <- srv.Shutdown(ctx)
	}()

	a.logger.Info("server started", "addr", srv.Addr, "env", a.config.Env)

	err := srv.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	err = <-shutdown
	if err != nil {
		return err
	}

	a.logger.Info("server stopped", "addr", srv.Addr, "env", a.config.Env)

	return nil
}
