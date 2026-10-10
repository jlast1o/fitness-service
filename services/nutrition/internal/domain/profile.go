package domain

import "time"

type NutritionProfile struct {
	UserID        string    `json:"user_id"`
	Age           int       `json:"age"`
	Sex           string    `json:"sex"`
	HeightCm      float64   `json:"height_cm"`
	WeightKg      float64   `json:"weight_kg"`
	ActivityLevel string    `json:"activity_level"`
	Goal          string    `json:"goal"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
