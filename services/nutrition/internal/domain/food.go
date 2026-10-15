package domain

import "time"

// Food хранит пищевую ценность продукта на 100 граммов.
type Food struct {
	ID              string    `json:"id"`
	OwnerUserID     string    `json:"owner_user_id,omitempty"`
	Name            string    `json:"name"`
	Brand           string    `json:"brand,omitempty"`
	CaloriesPer100g float64   `json:"calories_per_100g"`
	ProteinPer100g  float64   `json:"protein_per_100g"`
	FatPer100g      float64   `json:"fat_per_100g"`
	CarbsPer100g    float64   `json:"carbs_per_100g"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
