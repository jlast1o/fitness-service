package service

import (
	"context"
	"fmt"
	"time"

	"fitness-platform/services/planner/internal/domain"
	"fitness-platform/services/planner/internal/repository"

	"github.com/google/uuid"
)

// PlanGenerator отвечает за создание тренировочных планов.
type PlanGenerator struct {
	repo repository.PlannerRepository
}

// NewPlanGenerator создаёт новый генератор.
func NewPlanGenerator(repo repository.PlannerRepository) *PlanGenerator {
	return &PlanGenerator{repo: repo}
}

// GeneratePlan создаёт план на основе профиля пользователя.
// Возвращает готовый план вместе с неделями, днями и упражнениями.
func (g *PlanGenerator) GeneratePlan(ctx context.Context, profile *domain.UserProfile, startDate time.Time, durationWeeks int) (*domain.TrainingPlan, []domain.PlanWeek, []domain.PlanDay, []domain.PlannedExercise, error) {
	available, err := g.repo.ListAvailableExercises(ctx)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("list available exercises: %w", err)
	}

	selected, err := selectExercises(profile, available)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Выбираем правило прогрессии на основе уровня
	var progressionRule string
	var weeks []domain.PlanWeek
	var days []domain.PlanDay
	var planned []domain.PlannedExercise

	switch profile.ExperienceLevel {
	case "beginner":
		progressionRule = "linear"
		weeks, days, planned, err = g.generateLinear(profile, selected, startDate, durationWeeks)
	case "intermediate":
		progressionRule = "wave"
		weeks, days, planned, err = g.generateWave(profile, selected, startDate, durationWeeks)
	case "advanced":
		progressionRule = "rpe"
		weeks, days, planned, err = g.generateRPE(profile, selected, startDate, durationWeeks)
	default:
		return nil, nil, nil, nil, fmt.Errorf("unknown experience level: %s", profile.ExperienceLevel)
	}
	if err != nil {
		return nil, nil, nil, nil, err
	}

	plan := &domain.TrainingPlan{
		UserID:          profile.UserID,
		Name:            fmt.Sprintf("Plan for %s", profile.Goal),
		Goal:            profile.Goal,
		ExperienceLevel: profile.ExperienceLevel,
		StartDate:       startDate,
		EndDate:         startDate.AddDate(0, 0, durationWeeks*7),
		Status:          "active",
		ProgressionRule: progressionRule,
	}

	return plan, weeks, days, planned, nil
}

// selectExercises выбирает упражнения, подходящие под цель и ограничения.
func selectExercises(profile *domain.UserProfile, available []domain.AvailableExercise) ([]domain.AvailableExercise, error) {
	var result []domain.AvailableExercise
	for _, ex := range available {
		// Проверяем травмы: если есть ограничение на мышечную группу, пропускаем
		if isInjured(profile.Injuries, ex.MuscleGroup) {
			continue
		}
		// По цели отбираем: для силы — базовые, для похудения — любые, но с большим объёмом
		if profile.Goal == "strength" && ex.Category != "compound" {
			continue
		}
		if profile.Goal == "fat_loss" && ex.Category == "compound" && ex.MuscleGroup != "Core" {
			// Для похудения тоже можно базовые, но не все; оставим только базовые на крупные группы
			if ex.MuscleGroup != "Legs" && ex.MuscleGroup != "Chest" && ex.MuscleGroup != "Back" {
				continue
			}
		}
		result = append(result, ex)
	}
	if len(result) < 3 {
		return nil, fmt.Errorf("not enough exercises after filtering")
	}
	return result, nil
}

// isInjured проверяет, есть ли ограничение на мышечную группу.
func isInjured(injuries map[string]any, muscleGroup string) bool {
	if injuries == nil {
		return false
	}
	val, ok := injuries[muscleGroup]
	if !ok {
		return false
	}
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}

// generateLinear создаёт план для новичков с линейной прогрессией.
func (g *PlanGenerator) generateLinear(profile *domain.UserProfile, exercises []domain.AvailableExercise, startDate time.Time, durationWeeks int) ([]domain.PlanWeek, []domain.PlanDay, []domain.PlannedExercise, error) {
	weeks := make([]domain.PlanWeek, 0, durationWeeks)
	days := make([]domain.PlanDay, 0, durationWeeks*profile.DaysPerWeek)
	plannedExercises := make([]domain.PlannedExercise, 0, durationWeeks*profile.DaysPerWeek*len(exercises))

	// Определяем шаблон тренировочного дня (Full Body для новичка)
	// Количество дней в неделю = profile.DaysPerWeek, распределяем упражнения равномерно
	// Для простоты: каждый день выполняются все выбранные упражнения (Full Body)
	// Но если упражнений много, можно разделить на A/B, пока сделаем Full Body.
	for weekNum := 1; weekNum <= durationWeeks; weekNum++ {
		// Создаём неделю
		weekID := uuid.NewString()
		week := domain.PlanWeek{
			ID:         weekID,
			WeekNumber: weekNum,
			Focus:      "volume",
		}
		weeks = append(weeks, week)

		// Рассчитываем прибавку к весу для этой недели
		// Линейная: +2.5 кг каждую неделю
		weightIncrement := float64(weekNum-1) * 2.5

		// Для каждого тренировочного дня в неделе
		for dayNum := 1; dayNum <= profile.DaysPerWeek; dayNum++ {
			// Дата тренировки: startDate + (weekNum-1)*7 + (dayNum-1)
			dayID := uuid.NewString()
			dayDate := startDate.AddDate(0, 0, (weekNum-1)*7+(dayNum-1))
			day := domain.PlanDay{
				ID:        dayID,
				WeekID:    week.ID, // Будет проставлен после вставки недели
				DayNumber: dayNum,
				Date:      dayDate,
				Name:      fmt.Sprintf("Тренировка %d", dayNum),
			}
			days = append(days, day)

			// Для каждого упражнения в дне
			for order, exercise := range exercises {
				// Стартовый вес: если есть 1RM, берём 60%, иначе 20 кг
				plannedID := uuid.NewString()
				startWeight := 20.0
				if oneRM, ok := profile.Current1RM[exercise.ID]; ok && oneRM > 0 {
					startWeight = oneRM * 0.6
				}
				targetWeight := startWeight + weightIncrement

				// Подходы и повторения в зависимости от цели
				var sets, repsMin, repsMax int
				if profile.Goal == "strength" {
					sets, repsMin, repsMax = 5, 5, 5
				} else { // fat_loss
					sets, repsMin, repsMax = 3, 10, 15
				}

				planned := domain.PlannedExercise{
					ID:            plannedID,
					DayID:         day.ID, // Проставится после вставки дня
					ExerciseID:    exercise.ID,
					TargetSets:    sets,
					TargetRepsMin: repsMin,
					TargetRepsMax: repsMax,
					TargetWeight:  targetWeight,
					OrderIndex:    order + 1,
				}
				plannedExercises = append(plannedExercises, planned)
			}
		}
	}

	return weeks, days, plannedExercises, nil
}

// generateWave создаёт план для среднего уровня с волновой периодизацией.
func (g *PlanGenerator) generateWave(profile *domain.UserProfile, exercises []domain.AvailableExercise, startDate time.Time, durationWeeks int) ([]domain.PlanWeek, []domain.PlanDay, []domain.PlannedExercise, error) {
	weeks := make([]domain.PlanWeek, 0, durationWeeks)
	days := make([]domain.PlanDay, 0, durationWeeks*profile.DaysPerWeek)
	plannedExercises := make([]domain.PlannedExercise, 0, durationWeeks*profile.DaysPerWeek*len(exercises))

	// Цикл волн: 3 недели — объёмная, интенсивная, средняя
	wavePattern := []struct {
		focus      string
		percent1RM float64
		sets       int
		reps       int
	}{
		{"volume", 0.75, 5, 5},
		{"intensity", 0.85, 3, 3},
		{"medium", 0.80, 5, 3},
	}

	for weekNum := 1; weekNum <= durationWeeks; weekNum++ {
		// Определяем фазу волны по номеру недели (цикл 3)
		waveIndex := (weekNum - 1) % len(wavePattern)
		phase := wavePattern[waveIndex]

		weekID := uuid.NewString()
		week := domain.PlanWeek{
			ID:         weekID,
			WeekNumber: weekNum,
			Focus:      phase.focus,
		}
		weeks = append(weeks, week)

		// Корректируем 1RM для каждой новой волны (увеличиваем на 2.5 кг после каждых 3 недель)
		// Упростим: каждые 3 недели к 1RM добавляем 2.5 кг
		waveNumber := (weekNum - 1) / len(wavePattern) // 0,0,0,1,1,1,...
		rmIncrement := float64(waveNumber) * 2.5

		for dayNum := 1; dayNum <= profile.DaysPerWeek; dayNum++ {
			dayID := uuid.NewString()
			dayDate := startDate.AddDate(0, 0, (weekNum-1)*7+(dayNum-1))
			day := domain.PlanDay{
				ID:        dayID,
				WeekID:    weekID,
				DayNumber: dayNum,
				Date:      dayDate,
				Name:      fmt.Sprintf("Тренировка %d", dayNum),
			}
			days = append(days, day)

			for order, exercise := range exercises {
				// Базовый 1RM упражнения
				oneRM := profile.Current1RM[exercise.ID]
				if oneRM <= 0 {
					oneRM = 40.0 // дефолт для среднего уровня
				}
				// Прибавляем прогрессию волны
				oneRM += rmIncrement

				// Рабочий вес = процент от 1RM
				targetWeight := oneRM * phase.percent1RM

				plannedID := uuid.NewString()
				planned := domain.PlannedExercise{
					ID:            plannedID,
					DayID:         dayID,
					ExerciseID:    exercise.ID,
					TargetSets:    phase.sets,
					TargetRepsMin: phase.reps,
					TargetRepsMax: phase.reps,
					TargetWeight:  targetWeight,
					OrderIndex:    order + 1,
				}
				plannedExercises = append(plannedExercises, planned)
			}
		}
	}

	return weeks, days, plannedExercises, nil
}

// generateRPE создаёт план для продвинутых с RPE-авторегуляцией.
func (g *PlanGenerator) generateRPE(profile *domain.UserProfile, exercises []domain.AvailableExercise, startDate time.Time, durationWeeks int) ([]domain.PlanWeek, []domain.PlanDay, []domain.PlannedExercise, error) {
	weeks := make([]domain.PlanWeek, 0, durationWeeks)
	days := make([]domain.PlanDay, 0, durationWeeks*profile.DaysPerWeek)
	plannedExercises := make([]domain.PlannedExercise, 0, durationWeeks*profile.DaysPerWeek*len(exercises))

	for weekNum := 1; weekNum <= durationWeeks; weekNum++ {
		weekID := uuid.NewString()
		week := domain.PlanWeek{
			ID:         weekID,
			WeekNumber: weekNum,
			Focus:      "rpe",
		}
		weeks = append(weeks, week)

		for dayNum := 1; dayNum <= profile.DaysPerWeek; dayNum++ {
			dayID := uuid.NewString()
			dayDate := startDate.AddDate(0, 0, (weekNum-1)*7+(dayNum-1))
			day := domain.PlanDay{
				ID:        dayID,
				WeekID:    weekID,
				DayNumber: dayNum,
				Date:      dayDate,
				Name:      fmt.Sprintf("Тренировка %d", dayNum),
			}
			days = append(days, day)

			for order, exercise := range exercises {
				// Стартовый рекомендуемый вес: 70% от 1ПМ (если есть)
				oneRM := profile.Current1RM[exercise.ID]
				if oneRM <= 0 {
					oneRM = 60.0 // дефолт для продвинутого
				}
				startWeight := oneRM * 0.7

				// Целевой RPE и диапазон повторений зависят от цели
				var targetRPE float64
				var repsMin, repsMax int
				if profile.Goal == "strength" {
					targetRPE = 8.0
					repsMin, repsMax = 4, 6
				} else { // fat_loss
					targetRPE = 7.0
					repsMin, repsMax = 10, 15
				}

				plannedID := uuid.NewString()
				planned := domain.PlannedExercise{
					ID:            plannedID,
					DayID:         dayID,
					ExerciseID:    exercise.ID,
					TargetSets:    4, // фиксированное количество подходов
					TargetRepsMin: repsMin,
					TargetRepsMax: repsMax,
					TargetWeight:  startWeight, // рекомендация, не обязательна
					TargetRPE:     targetRPE,
					Notes:         "Выбери вес по ощущению RPE",
					OrderIndex:    order + 1,
				}
				plannedExercises = append(plannedExercises, planned)
			}
		}
	}

	return weeks, days, plannedExercises, nil
}
