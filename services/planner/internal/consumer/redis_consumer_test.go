package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/testutil/redisstreamtest"
)

func TestRecoverPendingBatch_PaginatesAndACKs(
	t *testing.T,
) {
	client := redisstreamtest.NewClient(t)

	const (
		stream = "workout.events"
		group  = "planner-recovery-test"
	)

	ctx := context.Background()

	// Создаём три pending-сообщения.
	redisstreamtest.MakePending(
		t,
		client,
		stream,
		group,
		`{
			"event_id":"22222222-2222-2222-2222-222222222222",
			"event_type":"test.ignored",
			"event_version":1,
			"payload":{}
		}`,
		3,
	)

	c := &RedisConsumer{
		redisClient:  client,
		stream:       stream,
		group:        group,
		consumer:     "planner-recovery",
		claimMinIdle: time.Millisecond,
		claimCount:   1,
		claimStart:   "0-0",
	}

	time.Sleep(25 * time.Millisecond)

	// Count=1 ограничивает число сообщений,
	// возвращаемых за один XAUTOCLAIM.
	//
	// Повторяем recovery, продвигаясь по cursor.
	for i := 0; i < 5 &&
		redisstreamtest.PendingCount(t, client, stream, group) > 0; i++ {

		c.recoverPendingBatch(ctx)
	}

	// Все три сообщения должны получить XACK.
	require.Zero(
		t,
		redisstreamtest.PendingCount(t, client, stream, group),
	)

	// После полного обхода Redis возвращает 0-0.
	require.Equal(t, "0-0", c.claimStart)
}
