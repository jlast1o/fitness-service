package domain

import "time"

type Exercise struct {
	ID          string
	Name        string
	MuscleGroup string
	Category    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Workout struct {
	ID         string
	UserID     string
	Name       string
	Date       time.Time
	Notes      string
	ProgramID  *string
	TemplateID *string
	Metrics    map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ExerciseSet struct {
	ID         string
	WorkoutID  string
	ExerciseID string
	OrderIndex int
	Weight     float64
	Reps       int
	RPE        float64
	Metrics    map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
