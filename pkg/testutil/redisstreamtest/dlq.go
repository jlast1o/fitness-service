package redisstreamtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"fitness-platform/pkg/eventdlq"
)

// Проверяет реальную цепочку:
// XAUTOCLAIM -> валидация -> DLQ -> XACK.
//
// runRecover вызывает recoverPendingBatch()
// конкретного consumer.
func AssertConsumerDLQ(
	t *testing.T,
	runRecover func(context.Context, *redis.Client, string, string) error,
) {
	t.Helper()

	client := NewClient(t)
	ctx := context.Background()

	tests := []struct {
		name       string
		raw        string
		wantReason string
	}{
		{
			name:       "invalid JSON envelope",
			raw:        "{bad",
			wantReason: eventdlq.InvalidEnvelope,
		},
		{
			name: "missing event ID",
			raw: `{
				"event_type": "workout.created",
				"event_version": 1,
				"payload": {}
			}`,
			wantReason: eventdlq.InvalidEnvelope,
		},
		{
			name: "unsupported version",
			raw: `{
				"event_id": "e1",
				"event_type": "workout.created",
				"event_version": 99,
				"payload": {}
			}`,
			wantReason: eventdlq.UnsupportedVersion,
		},
		{
			name: "invalid workout payload",
			raw: `{
				"event_id": "e1",
				"event_type": "workout.created",
				"event_version": 1,
				"payload": "not-an-object"
			}`,
			wantReason: eventdlq.InvalidPayload,
		},
		{
			name: "unknown valid event is ignored",
			raw: `{
				"event_id": "e1",
				"event_type": "test.ignored",
				"event_version": 1,
				"payload": {}
			}`,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := fmt.Sprintf("dlq.consumer.test.%d", i)
			group := "dlq.consumer.group"

			// Создаём реальное pending-сообщение.
			ids := MakePending(
				t, client, stream, group, tt.raw, 1,
			)

			// MinIdle в consumer установим равным 1ms.
			time.Sleep(25 * time.Millisecond)

			// Запускаем recovery конкретного сервиса.
			require.NoError(
				t,
				runRecover(ctx, client, stream, group),
			)

			// После корректной обработки оригинал
			// должен исчезнуть из PEL.
			require.Zero(
				t,
				PendingCount(t, client, stream, group),
			)

			entries, err := client.XRange(
				ctx,
				eventdlq.StreamName(stream, group),
				"-",
				"+",
			).Result()
			require.NoError(t, err)

			// test.ignored должен получить обычный ACK.
			// Для него DLQ не создаётся.
			if tt.wantReason == "" {
				require.Empty(t, entries)
				return
			}

			// Poison message должен попасть в DLQ.
			require.Len(t, entries, 1)

			require.Equal(
				t, tt.wantReason,
				entries[0].Values["reason"],
			)
			require.Equal(
				t, ids[0],
				entries[0].Values["source_message_id"],
			)
			require.Equal(
				t, stream,
				entries[0].Values["source_stream"],
			)
			require.Equal(
				t, group,
				entries[0].Values["source_group"],
			)
		})
	}
}
