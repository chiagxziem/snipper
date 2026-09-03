package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/goziemsunday/gater/internal/cache"
	"github.com/goziemsunday/gater/internal/config"
	"github.com/goziemsunday/gater/internal/db"
	"github.com/goziemsunday/gater/internal/mailer"
	"github.com/goziemsunday/gater/internal/store"
	"github.com/goziemsunday/gater/internal/validator"
	"github.com/goziemsunday/gater/internal/worker"
	"github.com/hibiken/asynq"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// load app config
	cfg, err := config.Load()
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	// database
	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		logger.Error("failed to create db pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	logger.Info("database connection pool established")

	dbStore := store.New(pool)

	emailer := mailer.NewResendClient(cfg)

	validator := validator.New()

	redisClient, err := cache.NewRedisClient(ctx, cfg)
	if err != nil {
		logger.Error("failed to create redis client", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// must be init after mailer & dbStore and before worker server
	workerClient := worker.NewClient(redisClient)
	defer workerClient.Close()

	workerServer := worker.NewServer(redisClient)
	go func() {
		err := workerServer.Run(worker.NewServeMux(emailer, dbStore, workerClient, logger))
		if err != nil {
			logger.Error("failed to run worker (asynq) server", "error", err)
			cancel()
			// os.Exit(1)
		}
	}()
	defer workerServer.Shutdown()

	// set up worker scheduler for periodic tasks
	workerScheduler := worker.NewScheduler(redisClient)

	endExpiredEventsEntryID, err := workerScheduler.Register(
		"@every 5m",
		asynq.NewTask(worker.TypeEndExpiredEvents, nil),
	)
	if err != nil {
		logger.Error(
			"failed to run register periodic worker task",
			"error", err,
			"task", worker.TypeEndExpiredEvents,
		)
		os.Exit(1)
	}
	logger.Info("registered a periodic worker entry", "entry_id", endExpiredEventsEntryID)

	expireWaitlistReservationID, err := workerScheduler.Register(
		"@every 5m",
		asynq.NewTask(worker.TypeExpireWaitlistReservations, nil),
	)
	if err != nil {
		logger.Error(
			"failed to run register periodic worker task",
			"error", err,
			"task", worker.TypeExpireWaitlistReservations,
		)
		os.Exit(1)
	}
	logger.Info("registered a periodic worker entry", "entry_id", expireWaitlistReservationID)

	// run scheduler
	go func() {
		if err := workerScheduler.Run(); err != nil {
			logger.Error("failed to run worker (asynq) scheduler", "error", err)
			cancel()
		}
	}()
	defer workerScheduler.Shutdown()

	// init app
	app := &application{
		config:    cfg,
		store:     dbStore,
		mailer:    emailer,
		worker:    workerClient,
		validator: validator,
		logger:    logger,
	}

	if err := app.run(app.mount()); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

}
