package handler

import (
	"encoding/json"
	"net/http"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/workout/internal/domain"
	"fitness-platform/services/workout/internal/service"
)

// ListExercises обрабатывает GET /exercises.
func (h *WorkoutHandler) ListExercises(w http.ResponseWriter, r *http.Request) {
	exercises, err := h.workoutService.ListExercises(r.Context())
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list exercises")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, exercises)
}

// CreateExercise обрабатывает POST /exercises.
func (h *WorkoutHandler) CreateExercise(w http.ResponseWriter, r *http.Request) {
	var req domain.Exercise
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	defer r.Body.Close()

	err := h.workoutService.CreateExercise(r.Context(), &req)
	if err != nil {
		if err == service.ErrInvalidWorkoutData {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		logger.Log.Error().Err(err).Msg("failed to create exercise")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, req)
}
