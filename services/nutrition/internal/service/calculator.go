package service

import "fitness-platform/services/nutrition/internal/domain"

type NutritionTargets struct {
	BMR           float64 `json:"bmr"`
	TDEE          float64 `json:"tdee"`
	DailyCalories float64 `json:"daily_calories"`
	ProteinGrams  float64 `json:"protein_grams"`
	FatGrams      float64 `json:"fat_grams"`
	CarbsGrams    float64 `json:"carbs_grams"`
}

func CalculateBMR(
	profile *domain.NutritionProfile,
) (float64, error) {
	if err := validateBodyMeasurements(profile); err != nil {
		return 0, err
	}

	base := 10*profile.WeightKg +
		6.25*profile.HeightCm -
		5*float64(profile.Age)

	if profile.Sex == "male" {
		return base + 5, nil
	}

	return base - 161, nil
}

func CalculateTDEE(
	profile *domain.NutritionProfile,
) (float64, error) {
	bmr, err := CalculateBMR(profile)
	if err != nil {
		return 0, err
	}

	multiplier, err := activityMultiplier(
		profile.ActivityLevel,
	)
	if err != nil {
		return 0, err
	}

	tdee := bmr * multiplier

	return tdee, nil
}

func CalculateTargets(
	profile *domain.NutritionProfile,
) (*NutritionTargets, error) {
	bmr, err := CalculateBMR(profile)
	if err != nil {
		return nil, err
	}

	tdee, err := CalculateTDEE(profile)
	if err != nil {
		return nil, err
	}

	multiplier, err := goalMultiplier(profile.Goal)
	if err != nil {
		return nil, err
	}

	calories := tdee * multiplier

	protein := calories * 0.25 / 4
	fat := calories * 0.30 / 9
	carbs := calories * 0.45 / 4

	return &NutritionTargets{
		BMR:           bmr,
		TDEE:          tdee,
		DailyCalories: calories,
		ProteinGrams:  protein,
		FatGrams:      fat,
		CarbsGrams:    carbs,
	}, nil
}

func activityMultiplier(level string) (float64, error) {
	switch level {
	case "sedentary":
		return 1.2, nil

	case "light":
		return 1.375, nil

	case "moderate":
		return 1.55, nil

	case "high":
		return 1.725, nil

	case "very_high":
		return 1.9, nil

	default:
		return 0, ErrInvalidInput
	}
}

func goalMultiplier(goal string) (float64, error) {
	switch goal {
	case "lose":
		return 0.9, nil

	case "maintain":
		return 1.0, nil

	case "gain":
		return 1.1, nil

	default:
		return 0, ErrInvalidInput
	}
}
