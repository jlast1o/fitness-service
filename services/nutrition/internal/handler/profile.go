package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/middleware"
	"fitness-platform/services/nutrition/internal/domain"
	"fitness-platform/services/nutrition/internal/service"
)

type NutritionHandler struct {
	nutritionService *service.NutritionService
}

func NewNutritionHandler(
	nutritionService *service.NutritionService,
) *NutritionHandler {
	return &NutritionHandler{
		nutritionService: nutritionService,
	}
}

type upsertProfileRequest struct {
	Age           int     `json:"age"`
	Sex           string  `json:"sex"`
	HeightCm      float64 `json:"height_cm"`
	WeightKg      float64 `json:"weight_kg"`
	ActivityLevel string  `json:"activity_level"`
	Goal          string  `json:"goal"`
}

func (h *NutritionHandler) UpsertProfile(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID := middleware.GetUserID(r.Context())

	if userID == "" {
		writeError(
			w,
			http.StatusUnauthorized,
			"user not authenticated",
		)
		return
	}

	var req upsertProfileRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			"invalid JSON body",
		)
		return
	}
	defer r.Body.Close()

	profile := &domain.NutritionProfile{
		UserID:        userID,
		Age:           req.Age,
		Sex:           req.Sex,
		HeightCm:      req.HeightCm,
		WeightKg:      req.WeightKg,
		ActivityLevel: req.ActivityLevel,
		Goal:          req.Goal,
	}

	savedProfile, err := h.nutritionService.UpsertProfile(
		r.Context(),
		profile,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidInput):
			writeError(
				w,
				http.StatusBadRequest,
				"invalid profile data",
			)

		default:
			logger.FromContext(r.Context()).
				Error().
				Err(err).
				Msg("failed to upsert nutrition profile")

			writeError(
				w,
				http.StatusInternalServerError,
				"internal server error",
			)
		}

		return
	}

	writeJSON(
		w,
		http.StatusOK,
		savedProfile,
	)
}

func (h *NutritionHandler) GetProfile(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID := middleware.GetUserID(r.Context())

	if userID == "" {
		writeError(
			w,
			http.StatusUnauthorized,
			"user not authenticated",
		)
		return
	}

	profile, err := h.nutritionService.GetProfile(
		r.Context(),
		userID,
	)
	if err != nil {
		logger.FromContext(r.Context()).
			Error().
			Err(err).
			Msg("failed to get nutrition profile")

		writeError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return
	}

	if profile == nil {
		writeError(
			w,
			http.StatusNotFound,
			"profile not found",
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		profile,
	)
}

// GetTargets возвращает рассчитанные нормы питания пользователя.
func (h *NutritionHandler) GetTargets(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID := middleware.GetUserID(r.Context())

	if userID == "" {
		writeError(
			w,
			http.StatusUnauthorized,
			"user not authenticated",
		)
		return
	}

	targets, err := h.nutritionService.GetTargets(
		r.Context(),
		userID,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(
				w,
				http.StatusBadRequest,
				"invalid profile data",
			)
			return
		}

		logger.FromContext(r.Context()).
			Error().
			Err(err).
			Msg("failed to calculate nutrition targets")

		writeError(
			w,
			http.StatusInternalServerError,
			"internal server error",
		)
		return
	}

	// Отсутствие профиля — ожидаемый сценарий,
	// а не внутренняя ошибка сервера.
	if targets == nil {
		writeError(
			w,
			http.StatusNotFound,
			"profile not found",
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		targets,
	)
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	value interface{},
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		logger.Log.Error().
			Err(err).
			Msg("failed to write JSON response")
	}
}

func writeError(
	w http.ResponseWriter,
	status int,
	message string,
) {
	writeJSON(
		w,
		status,
		map[string]string{
			"error": message,
		},
	)
}
