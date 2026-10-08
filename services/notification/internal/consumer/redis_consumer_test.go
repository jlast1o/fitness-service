package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/testutil/redisstreamtest"
	"fitness-platform/services/notification/internal/worker"
)

func TestRecoverPendingBatch_ACKsUnsupportedEvent(
	t *testing.T,
) {
	client := redisstreamtest.NewClient(t)

	const (
		stream = "workout.events"
		group  = "notification-recovery-ack-test"
	)

	ctx := context.Background()

	redisstreamtest.MakePending(
		t,
		client,
		stream,
		group,
		`{
			"event_id":"33333333-3333-3333-3333-333333333333",
			"event_type":"test.ignored",
			"event_version":1,
			"payload":{}
		}`,
		1,
	)

	c := &RedisConsumer{
		redisClient:  client,
		stream:       stream,
		group:        group,
		consumer:     "notification-recovery",
		claimMinIdle: time.Millisecond,
		claimCount:   10,
		claimStart:   "0-0",
	}

	time.Sleep(25 * time.Millisecond)

	c.recoverPendingBatch(ctx)

	require.Zero(
		t,
		redisstreamtest.PendingCount(t, client, stream, group),
	)
}

func TestRecoverPendingBatch_RejectedTaskStaysPending(
	t *testing.T,
) {
	client := redisstreamtest.NewClient(t)

	const (
		stream = "workout.events"
		group  = "notification-recovery-reject-test"
	)

	ctx := context.Background()

	ids := redisstreamtest.MakePending(
		t,
		client,
		stream,
		group,
		`{
			"event_id":"44444444-4444-4444-4444-444444444444",
			"event_type":"workout.created",
			"event_version":1,
			"payload":{
				"user_id":"55555555-5555-5555-5555-555555555555",
				"name":"Test workout"
			}
		}`,
		1,
	)

	// Остановленный worker pool отказывает в Submit.
	// Это имитирует временную невозможность
	// принять notification task.
	stoppedPool := worker.NewPool(
		ctx,
		nil,
		0,
		1,
	)

	require.NoError(t, stoppedPool.Shutdown(ctx))

	c := &RedisConsumer{
		redisClient:  client,
		stream:       stream,
		group:        group,
		consumer:     "notification-recovery",
		pool:         stoppedPool,
		claimMinIdle: time.Millisecond,
		claimCount:   10,
		claimStart:   "0-0",
	}

	time.Sleep(25 * time.Millisecond)

	c.recoverPendingBatch(ctx)

	// Submit отклонён: XACK выполнен быть не должен.
	require.EqualValues(
		t,
		1,
		redisstreamtest.PendingCount(t, client, stream, group),
	)

	pending, err := client.XPendingExt(
		ctx,
		&redis.XPendingExtArgs{
			Stream: stream,
			Group:  group,
			Start:  "-",
			End:    "+",
			Count:  10,
		},
	).Result()

	require.NoError(t, err)
	require.Len(t, pending, 1)

	// Сообщение всё ещё pending,
	// но теперь принадлежит recovery consumer'у.
	require.Equal(t, ids[0], pending[0].ID)
	require.Equal(t, "notification-recovery", pending[0].Consumer)
}
