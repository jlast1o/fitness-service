package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"fitness-platform/pkg/testutil/redisstreamtest"
)

func TestNotificationConsumer_BackoffAndRecovery(
	t *testing.T,
) {
	redisstreamtest.AssertBackoffAndRecovery(
		t,
		func(
			ctx context.Context,
			client *redis.Client,
			stream, group string,
		) {
			c := &RedisConsumer{
				redisClient:  client,
				stream:       stream,
				group:        group,
				consumer:     "notification-backoff-test",
				claimMinIdle: 30 * time.Second,
				claimStart:   "0-0",
			}

			c.Run(ctx)
		},
	)
}
