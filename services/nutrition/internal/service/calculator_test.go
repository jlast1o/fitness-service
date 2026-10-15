package service

import (
	"errors"
	"math"
	"testing"

	"fitness-platform/services/nutrition/internal/domain"
)

func TestCalculateBMR(t *testing.T) {
	tests := []struct {
		name string
		sex  string
		want float64
	}{
		{
			name: "male BMR",
			sex:  "male",
			want: 1555,
		},
		{
			name: "female BMR",
			sex:  "female",
			want: 1389,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := validTestProfile()
			profile.Sex = tt.sex

			got, err := CalculateBMR(profile)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Fatalf(
					"expected BMR %v, got %v",
					tt.want,
					got,
				)
			}
		})
	}
}

func TestCalculateBMRInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		change func(*domain.NutritionProfile)
	}{
		{
			name: "invalid age",
			change: func(p *domain.NutritionProfile) {
				p.Age = -5
			},
		},
		{
			name: "invalid height",
			change: func(p *domain.NutritionProfile) {
				p.HeightCm = 0
			},
		},
		{
			name: "invalid weight",
			change: func(p *domain.NutritionProfile) {
				p.WeightKg = 0
			},
		},
		{
			name: "invalid sex",
			change: func(p *domain.NutritionProfile) {
				p.Sex = "unknown"
			},
		},
		{
			name: "weight is NaN",
			change: func(p *domain.NutritionProfile) {
				p.WeightKg = math.NaN()
			},
		},
		{
			name: "height is infinity",
			change: func(p *domain.NutritionProfile) {
				p.HeightCm = math.Inf(1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := validTestProfile()
			tt.change(profile)

			_, err := CalculateBMR(profile)

			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf(
					"expected ErrInvalidInput, got %v",
					err,
				)
			}
		})
	}

	t.Run("nil profile", func(t *testing.T) {
		_, err := CalculateBMR(nil)

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf(
				"expected ErrInvalidInput, got %v",
				err,
			)
		}
	})
}

func TestCalculateBMRIndependentOfActivity(t *testing.T) {
	profile := validTestProfile()

	profile.ActivityLevel = "high"
	highBMR, err := CalculateBMR(profile)
	if err != nil {
		t.Fatal(err)
	}

	profile.ActivityLevel = "sedentary"
	lowBMR, err := CalculateBMR(profile)
	if err != nil {
		t.Fatal(err)
	}

	if highBMR != lowBMR {
		t.Fatal("activity level must not affect BMR")
	}
}

func TestCalculateBMRWithoutGoal(t *testing.T) {
	profile := validTestProfile()
	profile.Goal = ""
	profile.ActivityLevel = ""

	bmr, err := CalculateBMR(profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bmr != 1389 {
		t.Fatalf("expected BMR 1389, got %v", bmr)
	}
}

func TestCalculateTDEE(t *testing.T) {
	tests := []struct {
		name          string
		sex           string
		activityLevel string
		want          float64
	}{
		{
			name:          "female sedentary",
			sex:           "female",
			activityLevel: "sedentary",
			want:          1666.8,
		},
		{
			name:          "female light",
			sex:           "female",
			activityLevel: "light",
			want:          1909.875,
		},
		{
			name:          "female moderate",
			sex:           "female",
			activityLevel: "moderate",
			want:          2152.95,
		},
		{
			name:          "female high",
			sex:           "female",
			activityLevel: "high",
			want:          2396.025,
		},
		{
			name:          "female very high",
			sex:           "female",
			activityLevel: "very_high",
			want:          2639.1,
		},
		{
			name:          "male moderate",
			sex:           "male",
			activityLevel: "moderate",
			want:          2410.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := validTestProfile()
			profile.Sex = tt.sex
			profile.ActivityLevel = tt.activityLevel

			got, err := CalculateTDEE(profile)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if math.Abs(got-tt.want) > 0.000001 {
				t.Fatalf(
					"expected TDEE %v, got %v",
					tt.want,
					got,
				)
			}
		})
	}
}

func TestCalculateTDEEInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		change func(*domain.NutritionProfile)
	}{
		{
			name: "unknown activity level",
			change: func(p *domain.NutritionProfile) {
				p.ActivityLevel = "extreme"
			},
		},
		{
			name: "empty activity level",
			change: func(p *domain.NutritionProfile) {
				p.ActivityLevel = ""
			},
		},
		{
			name: "invalid age",
			change: func(p *domain.NutritionProfile) {
				p.Age = -5
			},
		},
		{
			name: "invalid weight",
			change: func(p *domain.NutritionProfile) {
				p.WeightKg = 0
			},
		},
		{
			name: "invalid height",
			change: func(p *domain.NutritionProfile) {
				p.HeightCm = 0
			},
		},
		{
			name: "invalid sex",
			change: func(p *domain.NutritionProfile) {
				p.Sex = "unknown"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := validTestProfile()
			tt.change(profile)

			_, err := CalculateTDEE(profile)

			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf(
					"expected ErrInvalidInput, got %v",
					err,
				)
			}
		})
	}

	t.Run("nil profile", func(t *testing.T) {
		_, err := CalculateTDEE(nil)

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf(
				"expected ErrInvalidInput, got %v",
				err,
			)
		}
	})
}

func TestCalculateTDEEWithoutGoal(t *testing.T) {
	profile := validTestProfile()

	profile.Goal = ""
	profile.UserID = ""

	got, err := CalculateTDEE(profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := 2152.95

	if math.Abs(got-want) > 0.000001 {
		t.Fatalf(
			"expected TDEE %v, got %v",
			want,
			got,
		)
	}
}

func TestCalculateTargets(t *testing.T) {
	tests := []struct {
		name         string
		goal         string
		wantCalories float64
	}{
		{
			name:         "lose weight",
			goal:         "lose",
			wantCalories: 1937.655,
		},
		{
			name:         "maintain weight",
			goal:         "maintain",
			wantCalories: 2152.95,
		},
		{
			name:         "gain weight",
			goal:         "gain",
			wantCalories: 2368.245,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := validTestProfile()
			profile.Goal = tt.goal

			got, err := CalculateTargets(profile)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if math.Abs(got.DailyCalories-tt.wantCalories) > 0.000001 {
				t.Fatalf(
					"expected calories %v, got %v",
					tt.wantCalories,
					got.DailyCalories,
				)
			}

			if math.Abs(got.BMR-1389) > 0.000001 {
				t.Fatalf("unexpected BMR: %v", got.BMR)
			}

			if math.Abs(got.TDEE-2152.95) > 0.000001 {
				t.Fatalf("unexpected TDEE: %v", got.TDEE)
			}

			totalCalories :=
				got.ProteinGrams*4 +
					got.FatGrams*9 +
					got.CarbsGrams*4

			if math.Abs(totalCalories-got.DailyCalories) > 0.000001 {
				t.Fatalf(
					"macros contain %v calories, expected %v",
					totalCalories,
					got.DailyCalories,
				)
			}
		})
	}
}

func TestCalculateTargetsInvalidInput(t *testing.T) {
	t.Run("nil profile", func(t *testing.T) {
		_, err := CalculateTargets(nil)

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf(
				"expected ErrInvalidInput, got %v",
				err,
			)
		}
	})

	t.Run("invalid goal", func(t *testing.T) {
		profile := validTestProfile()
		profile.Goal = "pizza"

		_, err := CalculateTargets(profile)

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf(
				"expected ErrInvalidInput, got %v",
				err,
			)
		}
	})

	t.Run("invalid activity", func(t *testing.T) {
		profile := validTestProfile()
		profile.ActivityLevel = "extreme"

		_, err := CalculateTargets(profile)

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf(
				"expected ErrInvalidInput, got %v",
				err,
			)
		}
	})

	t.Run("invalid weight", func(t *testing.T) {
		profile := validTestProfile()
		profile.WeightKg = -10

		_, err := CalculateTargets(profile)

		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf(
				"expected ErrInvalidInput, got %v",
				err,
			)
		}
	})
}

func TestCalculateTargetsWithoutUserID(t *testing.T) {
	profile := validTestProfile()
	profile.UserID = ""

	result, err := CalculateTargets(profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DailyCalories <= 0 {
		t.Fatal("daily calories must be positive")
	}
}
