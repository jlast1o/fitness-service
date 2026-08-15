package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/middleware"
	"fitness-platform/services/workout/internal/domain"
	"fitness-platform/services/workout/internal/service"
)

// WorkoutHandler обрабатывает HTTP-запросы, связанные с тренировками.
type WorkoutHandler struct {
	workoutService *service.WorkoutService
}

// NewWorkoutHandler создаёт новый экземпляр WorkoutHandler.
func NewWorkoutHandler(workoutService *service.WorkoutService) *WorkoutHandler {
	return &WorkoutHandler{workoutService: workoutService}
}

// createWorkoutRequest — тело запроса для создания тренировки.
type createWorkoutRequest struct {
	Name  string             `json:"name"`
	Date  time.Time          `json:"date"`
	Notes string             `json:"notes"`
	Sets  []createSetRequest `json:"sets"`
}

// createSetRequest — подход в запросе создания тренировки.
type createSetRequest struct {
	ExerciseID string  `json:"exercise_id"`
	Weight     float64 `json:"weight"`
	Reps       int     `json:"reps"`
	RPE        float64 `json:"rpe,omitempty"`
}

// CreateWorkout обрабатывает POST /workouts.
func (h *WorkoutHandler) CreateWorkout(w http.ResponseWriter, r *http.Request) {
	// 1. Получаем userID из контекста (добавлен middleware)
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	// 2. Декодируем JSON-тело
	var req createWorkoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	defer r.Body.Close()

	// 3. Преобразуем запрос в доменные структуры
	domainSets := make([]domain.ExerciseSet, 0, len(req.Sets))
	for _, s := range req.Sets {
		domainSets = append(domainSets, domain.ExerciseSet{
			ExerciseID: s.ExerciseID,
			Weight:     s.Weight,
			Reps:       s.Reps,
			RPE:        s.RPE,
		})
	}

	// 4. Вызываем сервис
	workout, err := h.workoutService.CreateWorkout(r.Context(), userID, req.Name, req.Date, req.Notes, domainSets)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidWorkoutData):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrExerciseNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			logger.Log.Error().Err(err).Msg("failed to create workout")
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	// 5. Отправляем успешный ответ
	writeJSON(w, http.StatusCreated, workout)
}

// GetWorkout обрабатывает GET /workouts/{id}.
func (h *WorkoutHandler) GetWorkout(w http.ResponseWriter, r *http.Request) {
	workoutID := chi.URLParam(r, "id")
	if workoutID == "" {
		writeError(w, http.StatusBadRequest, "missing workout id")
		return
	}

	workout, sets, err := h.workoutService.GetWorkout(r.Context(), workoutID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to get workout")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if workout == nil {
		writeError(w, http.StatusNotFound, "workout not found")
		return
	}

	// Формируем ответ: можно объединить workout и sets в одну структуру, но для простоты вернём workout и sets отдельно
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"workout": workout,
		"sets":    sets,
	})
}

// ListWorkouts обрабатывает GET /workouts.
func (h *WorkoutHandler) ListWorkouts(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	// Параметры пагинации из query string
	limit, offset := 20, 0
	// Здесь можно использовать r.URL.Query().Get, но для простоты пока оставим дефолты
	// В реальном коде стоит распарсить

	workouts, err := h.workoutService.ListWorkouts(r.Context(), userID, limit, offset)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to list workouts")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, workouts)
}

// DeleteWorkout обрабатывает DELETE /workouts/{id}.
func (h *WorkoutHandler) DeleteWorkout(w http.ResponseWriter, r *http.Request) {
	workoutID := chi.URLParam(r, "id")
	if workoutID == "" {
		writeError(w, http.StatusBadRequest, "missing workout id")
		return
	}

	err := h.workoutService.DeleteWorkout(r.Context(), workoutID)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to delete workout")
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
