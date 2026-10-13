package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTAuth_OnlyHS256(t *testing.T) {
	const secret = "test-secret"

	tests := []struct {
		name       string
		method     jwt.SigningMethod
		wantStatus int
	}{
		{
			name:       "HS256 accepted",
			method:     jwt.SigningMethodHS256,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "HS384 rejected",
			method:     jwt.SigningMethodHS384,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "HS512 rejected",
			method:     jwt.SigningMethodHS512,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := jwt.NewWithClaims(
				tt.method,
				jwt.MapClaims{
					"sub": "test-user",
					"exp": time.Now().Add(time.Hour).Unix(),
				},
			)

			signed, err := token.SignedString(
				[]byte(secret),
			)
			if err != nil {
				t.Fatal(err)
			}

			handler := JWTAuth(secret)(
				http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						if got := GetUserID(r.Context()); got != "test-user" {
							t.Errorf("unexpected user ID: %q", got)
						}

						w.WriteHeader(http.StatusNoContent)
					},
				),
			)

			req := httptest.NewRequest(
				http.MethodGet,
				"/protected",
				nil,
			)
			req.Header.Set("Authorization", "Bearer "+signed)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf(
					"got HTTP %d, want %d",
					rec.Code,
					tt.wantStatus,
				)
			}
		})
	}
}
