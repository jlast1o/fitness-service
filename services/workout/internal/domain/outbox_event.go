package domain

import "time"

// OutboxEvent — событие для публикации в шину (Redis Streams).
type OutboxEvent struct {
	ID          string
	EventType   string
	Payload     map[string]any
	CreatedAt   time.Time
	PublishedAt *time.Time
}
