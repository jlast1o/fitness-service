package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userIDKey contextKey = "userID"

// JWTAuth — middleware для проверки JWT-токена.
// Принимает секретный ключ и возвращает middleware-функцию.
func JWTAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Извлекаем токен из заголовка Authorization
			tokenString, err := extractToken(r)
			if err != nil {
				writeUnauthorized(w, "missing or invalid authorization header")
				return
			}

			// 2. Парсим токен, проверяем подпись и алгоритм
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				// Проверяем, что используется HMAC SHA256, чтобы избежать атак смены алгоритма
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				writeUnauthorized(w, "invalid token")
				return
			}

			// 3. Извлекаем claims
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeUnauthorized(w, "invalid token claims")
				return
			}

			// 4. Получаем userID из поля "sub"
			userID, ok := claims["sub"].(string)
			if !ok || userID == "" {
				writeUnauthorized(w, "missing user id in token")
				return
			}

			// 5. Добавляем userID в контекст запроса
			ctx := context.WithValue(r.Context(), userIDKey, userID)

			// 6. Вызываем следующий обработчик с обновлённым контекстом
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserID извлекает userID из контекста запроса.
// Возвращает пустую строку, если userID не найден.
func GetUserID(ctx context.Context) string {
	userID, _ := ctx.Value(userIDKey).(string)
	return userID
}

// extractToken достаёт токен из заголовка Authorization: Bearer <token>.
func extractToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", http.ErrNoCookie
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", http.ErrNoCookie
	}
	return parts[1], nil
}

// writeUnauthorized отправляет 401 Unauthorized с JSON-ошибкой.
func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + message + `"}`))
}
