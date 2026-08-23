package sender

import "context"

// Sender отправляет уведомление пользователю.
// Реализации могут быть: логгер, Telegram, email, Slack и т.д.
type Sender interface {
	Send(ctx context.Context, userID, message string) error
}
