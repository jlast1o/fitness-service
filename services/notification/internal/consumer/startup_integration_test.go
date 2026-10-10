package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/testutil/redisstreamtest"
)

func TestNotificationConsumer_ReadsMessageQueuedBeforeGroupCreation(t *testing.T) {
	client := redisstreamtest.NewClient(t)
	ctx := context.Background()
	const stream, group = "notification.startup.stream", "notification.startup.group"

	id, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{"event": `{"event_id":"00000000-0000-4000-8000-000000000013","event_type":"test.ignored","event_version":1,"payload":{}}`},
	}).Result()
	require.NoError(t, err)

	consumer := &RedisConsumer{
		redisClient: client, stream: stream, group: group,
		consumer: "notification-startup-test", claimMinIdle: time.Minute,
		claimStart: "0-0",
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); consumer.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("notification consumer did not stop")
		}
	})

	require.Eventually(t, func() bool {
		groups, err := client.XInfoGroups(ctx, stream).Result()
		return err == nil && len(groups) == 1 &&
			groups[0].LastDeliveredID == id && groups[0].Pending == 0
	}, 8*time.Second, 25*time.Millisecond)
}
