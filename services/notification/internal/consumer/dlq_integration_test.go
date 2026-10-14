package consumer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/eventdlq"
	"fitness-platform/pkg/testutil/redisstreamtest"
)

func TestRedisConsumer_PoisonMessagesGoToDLQ(t *testing.T) {
	client := redisstreamtest.NewClient(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		raw    string
		reason string
	}{
		{
			name:   "invalid JSON",
			raw:    "{bad",
			reason: eventdlq.InvalidEnvelope,
		},
		{
			name: "missing event ID",
			raw: `{
                "event_type": "workout.created",
                "event_version": 1,
                "payload": {}
            }`,
			reason: eventdlq.InvalidEnvelope,
		},
		{
			name: "unsupported version",
			raw: `{
                "event_id": "e1",
                "event_type": "workout.created",
                "event_version": 99,
                "payload": {}
            }`,
			reason: eventdlq.UnsupportedVersion,
		},
		{
			name: "invalid payload",
			raw: `{
                "event_id": "e1",
                "event_type": "workout.created",
                "event_version": 1,
                "payload": "not-an-object"
            }`,
			reason: eventdlq.InvalidPayload,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := fmt.Sprintf(
				"dlq.consumer.test.%d", i,
			)
			group := "dlq-test-group"

			ids := redisstreamtest.MakePending(
				t, client, stream, group, tt.raw, 1,
			)

			// Конструктор отличается по сервисам.
			c := newDLQTestConsumer(
				client, stream, group,
			)

			time.Sleep(25 * time.Millisecond)

			require.NoError(
				t, c.recoverPendingBatch(ctx),
			)

			// Исходное сообщение подтверждено.
			require.Zero(
				t,
				redisstreamtest.PendingCount(
					t, client, stream, group,
				),
			)

			entries, err := client.XRange(
				ctx,
				eventdlq.StreamName(stream, group),
				"-",
				"+",
			).Result()

			require.NoError(t, err)
			require.Len(t, entries, 1)

			require.Equal(
				t,
				tt.reason,
				entries[0].Values["reason"],
			)

			require.Equal(
				t,
				ids[0],
				entries[0].Values["source_message_id"],
			)
		})
	}
}

func newDLQTestConsumer(
	client *redis.Client,
	stream, group string,
) *RedisConsumer {
	return NewRedisConsumer(
		client, stream, group,
		"notification-dlq-test",
		time.Millisecond,
		nil,
	)
}
