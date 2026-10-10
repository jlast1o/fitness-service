package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"fitness-platform/services/nutrition/internal/domain"
)

func TestUpsertProfileValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*domain.NutritionProfile)
	}{
		{
			name: "age below minimum",
			change: func(p *domain.NutritionProfile) {
				p.Age = 17
			},
		},
		{
			name: "age above maximum",
			change: func(p *domain.NutritionProfile) {
				p.Age = 121
			},
		},
		{
			name: "height below minimum",
			change: func(p *domain.NutritionProfile) {
				p.HeightCm = 90
			},
		},
		{
			name: "height above maximum",
			change: func(p *domain.NutritionProfile) {
				p.HeightCm = 300
			},
		},
		{
			name: "weight below minimum",
			change: func(p *domain.NutritionProfile) {
				p.WeightKg = 10
			},
		},
		{
			name: "weight above maximum",
			change: func(p *domain.NutritionProfile) {
				p.WeightKg = 500
			},
		},
		{
			name: "invalid sex",
			change: func(p *domain.NutritionProfile) {
				p.Sex = "unknown"
			},
		},
		{
			name: "invalid activity level",
			change: func(p *domain.NutritionProfile) {
				p.ActivityLevel = "extreme"
			},
		},
		{
			name: "invalid goal",
			change: func(p *domain.NutritionProfile) {
				p.Goal = "pizza"
			},
		},
		{
			name: "empty user ID",
			change: func(p *domain.NutritionProfile) {
				p.UserID = ""
			},
		},
		{
			name: "height is NaN",
			change: func(p *domain.NutritionProfile) {
				p.HeightCm = math.NaN()
			},
		},
		{
			name: "weight is infinity",
			change: func(p *domain.NutritionProfile) {
				p.WeightKg = math.Inf(1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeNutritionRepository{}
			svc := NewNutritionService(repo)

			profile := validTestProfile()
			tt.change(profile)

			_, err := svc.UpsertProfile(
				context.Background(),
				profile,
			)

			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf(
					"expected ErrInvalidInput, got %v",
					err,
				)
			}

			if repo.upsertCalled {
				t.Fatal("invalid profile was saved")
			}

			if repo.getCalled {
				t.Fatal("repository was called")
			}
		})
	}
}

func TestUpsertProfileNilProfile(t *testing.T) {
	repo := &fakeNutritionRepository{}
	svc := NewNutritionService(repo)

	_, err := svc.UpsertProfile(
		context.Background(),
		nil,
	)

	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf(
			"expected ErrInvalidInput, got %v",
			err,
		)
	}

	if repo.upsertCalled {
		t.Fatal("repository must not be called")
	}
}
