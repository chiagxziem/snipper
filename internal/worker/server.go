package worker

import (
	"github.com/goziemsunday/gater/internal/mailer"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

func NewServer(rc *redis.Client) *asynq.Server {
	s := asynq.NewServerFromRedisClient(
		rc,
		asynq.Config{Concurrency: 10},
	)
	return s
}

func NewServeMux(m mailer.Mailer) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeSendVerificationEmail, HandleSendVerificationTask(m))
	mux.HandleFunc(TypeSendPasswordResetEmail, HandleSendPwdResetTask(m))

	return mux
}
