package handler

import (
	"encoding/json"
	"errors"
	"fitness-platform/pkg/logger"
	"fitness-platform/services/auth/internal/service"
	"net/http"
)

type AuthHandler struct {
	authService *service.AuthService
}

func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerResponse struct {
	UserID string `json:"user_id"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	defer r.Body.Close()

	userID, err := h.authService.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrEmailPassRequired) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		logger.Log.Error().Err(err).Msg("register handler failed")
		writeError(w, http.StatusInternalServerError, "interval server error")
		return
	}

	writeJSON(w, http.StatusCreated, registerResponse{UserID: userID})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	defer r.Body.Close()

	accessToken, refreshToken, err := h.authService.Login(r.Context(), req.Email, req.Password)

	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailPassRequired):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "invalid credentials")
		default:
			logger.Log.Error().Err(err).Msg("login handler failed")
			writeError(w, http.StatusInternalServerError, "interval service error")
		}
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

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
