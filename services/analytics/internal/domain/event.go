package domain

import "time"

// WorkoutCreatedEvent — событие о создании тренировки, приходящее из Workout Service.
type WorkoutCreatedEvent struct {
	WorkoutID string    `json:"workout_id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Date      time.Time `json:"date"`
	SetsCount int       `json:"sets_count"`
	Sets      []SetInfo `json:"sets,omitempty"` // опционально, если передаём детали
}

// SetInfo — информация о подходе в событии.
type SetInfo struct {
	ExerciseID string  `json:"exercise_id"`
	Weight     float64 `json:"weight"`
	Reps       int     `json:"reps"`
}
