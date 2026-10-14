package eventdlq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	InvalidEnvelope    = "invalid_envelope"
	UnsupportedVersion = "unsupported_version"
	InvalidPayload     = "invalid_payload"
)

func StreamName(sourceStream, group string) string {
	return sourceStream + ".dlq" + group
}

func PublishAndAck(ctx context.Context, client redis.Cmdable, sourceStream, group string, message redis.XMessage, reason, detail string) error {
	originalValues, err := json.Marshal(message.Values)
	if err != nil {
		return fmt.Errorf("marshal original redis message %s: %w", message.ID, err)
	}

	dlq := StreamName(sourceStream, group)

	err = client.XAdd(ctx, &redis.XAddArgs{
		Stream: dlq,
		Values: map[string]interface{}{
			"source_stream":     sourceStream,
			"source_group":      group,
			"source_message_id": message.ID,
			"reason":            reason,
			"detail":            detail,
			"original_values":   string(originalValues),
			"failed_at":         time.Now().UTC().Format(time.RFC3339Nano),
		},
	}).Err()

	if err != nil {
		return fmt.Errorf(
			"publish message %s to DLQ %q: %w",
			message.ID, dlq, err,
		)
	}

	if err := client.XAck(
		ctx,
		sourceStream,
		group,
		message.ID,
	).Err(); err != nil {
		return fmt.Errorf(
			"DLQ saved, but XACK of message %s failed: %w",
			message.ID, err,
		)
	}

	return nil
}
