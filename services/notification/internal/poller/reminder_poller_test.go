package poller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"fitness-platform/services/notification/internal/worker"
)

type MockSender struct {
	mock.Mock
}

func (m *MockSender) Send(ctx context.Context, userID, message string) error {
	args := m.Called(ctx, userID, message)
	return args.Error(0)
}

func TestPoll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-token", r.Header.Get("X-Internal-Token"))
		reminders := []struct {
			UserID  string `json:"user_id"`
			Message string `json:"message"`
		}{
			{UserID: "user1", Message: "Тренировка завтра"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reminders)
	}))
	defer server.Close()

	mockSender := new(MockSender)
	mockSender.On("Send", mock.Anything, "user1", "Тренировка завтра").Return(nil)

	pool := worker.NewPool(context.Background(), mockSender, 2, 10)
	pool.Start()
	defer pool.Shutdown(context.Background())

	p := NewReminderPoller(server.URL, pool, time.Hour, "test-token")
	p.poll(context.Background())

	time.Sleep(70 * time.Millisecond)

	mockSender.AssertExpectations(t)
}
