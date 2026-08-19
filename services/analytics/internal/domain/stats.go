package domain

import "time"

// UserStats — агрегированная статистика пользователя.
type UserStats struct {
	UserID        string    `json:"user_id"`
	TotalWorkouts int       `json:"total_workouts"`
	TotalVolume   float64   `json:"total_volume"`
	AvgIntensity  float64   `json:"avg_intensity"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ExerciseProgress — прогресс по конкретному упражнению.
type ExerciseProgress struct {
	UserID        string    `json:"user_id"`
	ExerciseID    string    `json:"exercise_id"`
	BestWeight    float64   `json:"best_weight"`
	TotalReps     int       `json:"total_reps"`
	LastWorkoutAt time.Time `json:"last_workout_at,omitempty"`
	Estimated1RM  float64   `json:"estimated_1rm"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// WorkoutSummary — денормализованная сводка тренировки для быстрого отображения.
type WorkoutSummary struct {
	WorkoutID   string    `json:"workout_id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Date        time.Time `json:"date"`
	TotalVolume float64   `json:"total_volume"`
	SetCount    int       `json:"set_count"`
	CreatedAt   time.Time `json:"created_at"`
}
