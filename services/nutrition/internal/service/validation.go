package service

import (
	"math"

	"fitness-platform/services/nutrition/internal/domain"
)

func validateBodyMeasurements(
	profile *domain.NutritionProfile,
) error {
	if profile == nil {
		return ErrInvalidInput
	}

	if profile.Age < 18 || profile.Age > 120 {
		return ErrInvalidInput
	}

	if math.IsNaN(profile.HeightCm) ||
		math.IsInf(profile.HeightCm, 0) ||
		profile.HeightCm < 100 ||
		profile.HeightCm > 250 {
		return ErrInvalidInput
	}

	if math.IsNaN(profile.WeightKg) ||
		math.IsInf(profile.WeightKg, 0) ||
		profile.WeightKg < 20 ||
		profile.WeightKg > 400 {
		return ErrInvalidInput
	}

	switch profile.Sex {
	case "male", "female":
	default:
		return ErrInvalidInput
	}

	return nil
}

func validateProfile(
	profile *domain.NutritionProfile,
) error {
	if err := validateBodyMeasurements(profile); err != nil {
		return err
	}

	if profile.UserID == "" {
		return ErrInvalidInput
	}

	if _, err := activityMultiplier(profile.ActivityLevel); err != nil {
		return err
	}

	if _, err := goalMultiplier(profile.Goal); err != nil {
		return err
	}

	return nil
}
