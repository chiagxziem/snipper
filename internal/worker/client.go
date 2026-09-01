package worker

import (
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

func NewClient(rc *redis.Client) *asynq.Client {
	c := asynq.NewClientFromRedisClient(rc)
	return c
}
