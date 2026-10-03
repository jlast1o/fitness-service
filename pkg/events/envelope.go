package events

import (
	"encoding/json"
	"time"
)

const (
	TypeWorkoutCreated = "workout.created"
	TypeWorkoutUpdated = "workout.updated"

	Version1 = 1
)

type Envelope struct {
	EventID      string          `json:"event_id"`
	EventType    string          `json:"event_type"`
	EventVersion int             `json:"event_version"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Payload      json.RawMessage `json:"payload"`
}
