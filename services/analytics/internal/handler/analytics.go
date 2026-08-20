package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/middleware"
	"fitness-platform/services/analytics/internal/service"
)

// AnalyticsHandler обрабатывает HTTP-запросы аналитики.
type AnalyticsHandler struct {
	analyticsService *service.AnalyticsService
}

// NewAnalyticsHandler создаёт новый экземпляр AnalyticsHandler.
func NewAnalyticsHandler(analyticsService *service.AnalyticsService) *AnalyticsHandler {
	return &AnalyticsHandler{analyticsService: analyticsService}
}

// Dashboard возвращает общую статистику пользователя и последние тренировки.
func (h *AnalyticsHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	stats, err := h.analyticsService.GetUserStats(r.Context(), userID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get user stats")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if stats == nil {
		writeError(w, http.StatusNotFound, "no stats found")
		return
	}

	// Получаем последние 5 тренировок
	summaries, err := h.analyticsService.ListWorkoutSummaries(r.Context(), userID, 5, 0)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list workout summaries")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stats":     stats,
		"summaries": summaries,
	})
}

// Progress возвращает прогресс по всем упражнениям или по конкретному.
func (h *AnalyticsHandler) Progress(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	exerciseID := r.URL.Query().Get("exercise_id")
	if exerciseID != "" {
		progress, err := h.analyticsService.GetExerciseProgress(r.Context(), userID, exerciseID)
		if err != nil {
			logger.Log.Error().Err(err).Msg("failed to get exercise progress")
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if progress == nil {
			writeError(w, http.StatusNotFound, "progress not found")
			return
		}
		writeJSON(w, http.StatusOK, progress)
		return
	}

	progressList, err := h.analyticsService.ListExerciseProgress(r.Context(), userID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list exercise progress")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, progressList)
}

// WorkoutHistory возвращает историю тренировок с пагинацией.
func (h *AnalyticsHandler) WorkoutHistory(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(offsetStr)
	if offset < 0 {
		offset = 0
	}

	summaries, err := h.analyticsService.ListWorkoutSummaries(r.Context(), userID, limit, offset)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list workout summaries")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

// writeJSON записывает ответ в формате JSON.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Log.Error().Err(err).Msg("failed to write JSON response")
	}
}

// writeError записывает ошибку в формате JSON.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
