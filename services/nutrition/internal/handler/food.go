package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"fitness-platform/pkg/logger"
	"fitness-platform/pkg/middleware"
	"fitness-platform/services/nutrition/internal/domain"
	"fitness-platform/services/nutrition/internal/service"

	"github.com/go-chi/chi/v5"
)

type FoodHandler struct {
	foodService *service.FoodService
}

func NewFoodHandler(foodService *service.FoodService) *FoodHandler {
	return &FoodHandler{foodService: foodService}
}

type createFoodRequest struct {
	Name            string   `json:"name"`
	Brand           string   `json:"brand"`
	CaloriesPer100g *float64 `json:"calories_per_100g"`
	ProteinPer100g  *float64 `json:"protein_per_100g"`
	FatPer100g      *float64 `json:"fat_per_100g"`
	CarbsPer100g    *float64 `json:"carbs_per_100g"`
}

// CreateFood обрабатывает создание личного продукта.
func (h *FoodHandler) CreateFood(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	food, ok := readFoodRequest(w, r)
	if !ok {
		return
	}

	created, err := h.foodService.CreateFood(
		r.Context(),
		userID,
		food,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid food data")
			return
		}

		logger.FromContext(r.Context()).
			Error().
			Err(err).
			Msg("failed to create food")

		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Location", "/nutrition/foods/"+created.ID)
	writeJSON(w, http.StatusCreated, created)
}

// ListFoods возвращает общие и личные продукты с пагинацией.
func (h *FoodHandler) ListFoods(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	limit := 20
	offset := 0

	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid pagination")
			return
		}
		limit = parsed
	}

	if value := r.URL.Query().Get("offset"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid pagination")
			return
		}
		offset = parsed
	}

	foods, err := h.foodService.ListFoods(
		r.Context(),
		userID,
		limit,
		offset,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid pagination")
			return
		}

		logger.FromContext(r.Context()).
			Error().
			Err(err).
			Msg("failed to list foods")

		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, foods)
}

// GetFood возвращает доступный пользователю продукт по ID.
func (h *FoodHandler) GetFood(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	foodID := chi.URLParam(r, "id")

	food, err := h.foodService.GetFood(
		r.Context(),
		userID,
		foodID,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid food ID")
			return
		}

		logger.FromContext(r.Context()).
			Error().
			Err(err).
			Msg("failed to get food")

		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if food == nil {
		writeError(w, http.StatusNotFound, "food not found")
		return
	}

	writeJSON(w, http.StatusOK, food)
}

// readFoodRequest читает и проверяет структуру запроса на создание или изменение продукта.
func readFoodRequest(
	w http.ResponseWriter,
	r *http.Request,
) (*domain.Food, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req createFoodRequest

	if err := decoder.Decode(&req); err != nil {
		writeFoodDecodeError(w, err)
		return nil, false
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		} else {
			writeFoodDecodeError(w, err)
		}
		return nil, false
	}

	if req.CaloriesPer100g == nil ||
		req.ProteinPer100g == nil ||
		req.FatPer100g == nil ||
		req.CarbsPer100g == nil {
		writeError(w, http.StatusBadRequest, "missing nutrition values")
		return nil, false
	}

	return &domain.Food{
		Name:            req.Name,
		Brand:           req.Brand,
		CaloriesPer100g: *req.CaloriesPer100g,
		ProteinPer100g:  *req.ProteinPer100g,
		FatPer100g:      *req.FatPer100g,
		CarbsPer100g:    *req.CarbsPer100g,
	}, true
}

// writeFoodDecodeError возвращает соответствующую ошибку чтения JSON.
func writeFoodDecodeError(w http.ResponseWriter, err error) {
	var sizeErr *http.MaxBytesError

	if errors.As(err, &sizeErr) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	writeError(w, http.StatusBadRequest, "invalid JSON body")
}

// UpdateFood изменяет личный продукт пользователя.
func (h *FoodHandler) UpdateFood(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	foodID := chi.URLParam(r, "id")

	food, ok := readFoodRequest(w, r)
	if !ok {
		return
	}

	updated, err := h.foodService.UpdateFood(
		r.Context(),
		userID,
		foodID,
		food,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid food data or ID")
			return
		}

		logger.FromContext(r.Context()).
			Error().
			Err(err).
			Msg("failed to update food")

		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if updated == nil {
		writeError(w, http.StatusNotFound, "food not found")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// DeleteFood мягко удаляет личный продукт пользователя.
func (h *FoodHandler) DeleteFood(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	foodID := chi.URLParam(r, "id")

	deleted, err := h.foodService.DeleteFood(
		r.Context(),
		userID,
		foodID,
	)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid food ID")
			return
		}

		logger.FromContext(r.Context()).
			Error().
			Err(err).
			Msg("failed to delete food")

		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if !deleted {
		writeError(w, http.StatusNotFound, "food not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
