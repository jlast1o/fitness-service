package sender

import (
	"context"

	"fitness-platform/pkg/logger"
)

// LoggerSender — простая реализация Sender, которая логирует уведомления.
// Используется на этапе разработки, пока нет реального канала доставки.
type LoggerSender struct{}

// NewLoggerSender создаёт LoggerSender.
func NewLoggerSender() *LoggerSender {
	return &LoggerSender{}
}

// Send логирует сообщение и возвращает nil.
func (s *LoggerSender) Send(ctx context.Context, userID, message string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		logger.Log.Info().
			Str("user_id", userID).
			Str("message", message).
			Msg("Notification sent")
		return nil
	}
}
