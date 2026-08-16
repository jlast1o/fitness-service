package domain

import "time"

// Exercise — справочник упражнений.
type Exercise struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	MuscleGroup string    `json:"muscle_group"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Workout — тренировка пользователя.
type Workout struct {
	ID         string         `json:"id"`
	UserID     string         `json:"user_id"`
	Name       string         `json:"name"`
	Date       time.Time      `json:"date"`
	Notes      string         `json:"notes,omitempty"`
	ProgramID  *string        `json:"program_id,omitempty"`
	TemplateID *string        `json:"template_id,omitempty"`
	Metrics    map[string]any `json:"metrics,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// ExerciseSet — один подход в упражнении.
type ExerciseSet struct {
	ID         string         `json:"id"`
	WorkoutID  string         `json:"workout_id"`
	ExerciseID string         `json:"exercise_id"`
	OrderIndex int            `json:"order_index"`
	Weight     float64        `json:"weight"`
	Reps       int            `json:"reps"`
	RPE        float64        `json:"rpe,omitempty"`
	Metrics    map[string]any `json:"metrics,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}
