package handler

import (
	"net/http"
	"time"

	"fitness-platform/pkg/logger"
)

// RemindersHandler возвращает предстоящие тренировки для напоминаний.
// Защищён внутренним токеном.
func (h *PlannerHandler) Reminders(w http.ResponseWriter, r *http.Request) {
	// Проверяем внутренний токен
	internalToken := r.Header.Get("X-Internal-Token")
	if internalToken == "" || internalToken != "dev-secret-token" { // в реальности брать из конфига
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	now := time.Now()
	reminders, err := h.plannerService.GetUpcomingWorkouts(r.Context(), now, now.Add(24*time.Hour))
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get upcoming workouts")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, reminders)
}
