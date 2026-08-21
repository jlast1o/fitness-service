package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/middleware"
	"fitness-platform/services/planner/internal/domain"
	"fitness-platform/services/planner/internal/service"
)

// PlannerHandler обрабатывает HTTP-запросы планировщика.
type PlannerHandler struct {
	plannerService *service.PlannerService
}

// NewPlannerHandler создаёт новый экземпляр PlannerHandler.
func NewPlannerHandler(plannerService *service.PlannerService) *PlannerHandler {
	return &PlannerHandler{plannerService: plannerService}
}

// upsertProfileRequest — тело запроса для создания/обновления профиля.
type upsertProfileRequest struct {
	Goal            string             `json:"goal"`
	ExperienceLevel string             `json:"experience_level"`
	DaysPerWeek     int                `json:"days_per_week"`
	Injuries        map[string]any     `json:"injuries,omitempty"`
	Current1RM      map[string]float64 `json:"current_1rm,omitempty"`
}

// UpsertProfile обрабатывает POST /planner/profile.
func (h *PlannerHandler) UpsertProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	var req upsertProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	defer r.Body.Close()

	profile := &domain.UserProfile{
		UserID:          userID,
		Goal:            req.Goal,
		ExperienceLevel: req.ExperienceLevel,
		DaysPerWeek:     req.DaysPerWeek,
		Injuries:        req.Injuries,
		Current1RM:      req.Current1RM,
	}

	if err := h.plannerService.UpsertProfile(r.Context(), profile); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			logger.Log.Error().Err(err).Msg("failed to upsert profile")
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	writeJSON(w, http.StatusOK, profile)
}

// GetProfile обрабатывает GET /planner/profile.
func (h *PlannerHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	profile, err := h.plannerService.GetProfile(r.Context(), userID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get profile")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if profile == nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}

	writeJSON(w, http.StatusOK, profile)
}

// ListExercises обрабатывает GET /planner/exercises.
func (h *PlannerHandler) ListExercises(w http.ResponseWriter, r *http.Request) {
	exercises, err := h.plannerService.ListExercises(r.Context())
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list exercises")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, exercises)
}

// generatePlanRequest — тело запроса для генерации плана.
type generatePlanRequest struct {
	StartDate     time.Time `json:"start_date"`
	DurationWeeks int       `json:"duration_weeks"`
}

// GeneratePlan обрабатывает POST /planner/plans.
// GeneratePlan обрабатывает POST /planner/plans.
func (h *PlannerHandler) GeneratePlan(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	var req generatePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	defer r.Body.Close()

	if req.DurationWeeks < 1 || req.DurationWeeks > 12 {
		req.DurationWeeks = 4
	}
	if req.StartDate.IsZero() {
		req.StartDate = time.Now()
	}

	plan, err := h.plannerService.GenerateAndSavePlan(r.Context(), userID, req.StartDate, req.DurationWeeks)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			logger.Log.Error().Err(err).Msg("failed to generate plan")
			writeError(w, http.StatusInternalServerError, "failed to generate plan")
		}
		return
	}

	writeJSON(w, http.StatusCreated, plan)
}

// GetCurrentPlan обрабатывает GET /planner/plans/current.
func (h *PlannerHandler) GetCurrentPlan(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	plan, err := h.plannerService.GetActivePlan(r.Context(), userID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get active plan")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if plan == nil {
		writeError(w, http.StatusNotFound, "no active plan")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// GetNextWorkout обрабатывает GET /planner/next-workout.
func (h *PlannerHandler) GetNextWorkout(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	day, exercises, err := h.plannerService.GetNextWorkout(r.Context(), userID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get next workout")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if day == nil {
		writeError(w, http.StatusNotFound, "no upcoming workouts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"day":       day,
		"exercises": exercises,
	})
}

// Вспомогательные функции writeJSON и writeError
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Log.Error().Err(err).Msg("failed to write JSON response")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
