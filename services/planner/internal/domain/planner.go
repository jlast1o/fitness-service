package domain

import "time"

// UserProfile — тренировочный профиль пользователя.
type UserProfile struct {
	UserID          string             `json:"user_id"`
	Goal            string             `json:"goal"`             // strength, fat_loss
	ExperienceLevel string             `json:"experience_level"` // beginner, intermediate, advanced
	DaysPerWeek     int                `json:"days_per_week"`
	Injuries        map[string]any     `json:"injuries,omitempty"`    // ограничения: {"knee": true, ...}
	Current1RM      map[string]float64 `json:"current_1rm,omitempty"` // exercise_id -> 1RM
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// TrainingPlan — тренировочный план.
type TrainingPlan struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Name            string    `json:"name"`
	Goal            string    `json:"goal"`
	ExperienceLevel string    `json:"experience_level"`
	StartDate       time.Time `json:"start_date"`
	EndDate         time.Time `json:"end_date"`
	Status          string    `json:"status"`           // active, completed, paused
	ProgressionRule string    `json:"progression_rule"` // linear, wave, rpe
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PlanWeek — неделя тренировочного плана.
type PlanWeek struct {
	ID         string    `json:"id"`
	PlanID     string    `json:"plan_id"`
	WeekNumber int       `json:"week_number"`
	Focus      string    `json:"focus"` // volume, intensity, recovery
	CreatedAt  time.Time `json:"created_at"`
}

// PlanDay — тренировочный день.
type PlanDay struct {
	ID        string    `json:"id"`
	WeekID    string    `json:"week_id"`
	DayNumber int       `json:"day_number"`
	Date      time.Time `json:"date"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// PlannedExercise — запланированное упражнение в дне.
type PlannedExercise struct {
	ID            string    `json:"id"`
	DayID         string    `json:"day_id"`
	ExerciseID    string    `json:"exercise_id"`
	TargetSets    int       `json:"target_sets"`
	TargetRepsMin int       `json:"target_reps_min"`
	TargetRepsMax int       `json:"target_reps_max"`
	TargetWeight  float64   `json:"target_weight"`
	TargetRPE     float64   `json:"target_rpe,omitempty"`
	Notes         string    `json:"notes,omitempty"`
	OrderIndex    int       `json:"order_index"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AvailableExercise — упражнение из справочника Planner.
type AvailableExercise struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	MuscleGroup string    `json:"muscle_group"`
	Category    string    `json:"category"` // compound, isolation, cardio
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
