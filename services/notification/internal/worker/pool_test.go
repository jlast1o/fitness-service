package worker_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"fitness-platform/services/notification/internal/domain"
	"fitness-platform/services/notification/internal/worker"
)

// MockSender — мок отправителя.
type MockSender struct {
	mock.Mock
}

func (m *MockSender) Send(ctx context.Context, userID, message string) error {
	args := m.Called(ctx, userID, message)
	return args.Error(0)
}

func TestPool_SubmitAndProcess(t *testing.T) {
	mockSender := new(MockSender)
	mockSender.On("Send", mock.Anything, "user123", "Hello").Return(nil)

	ctx := context.Background()
	pool := worker.NewPool(ctx, mockSender, 2, 10)
	pool.Start()

	task := domain.NotificationTask{
		UserID:    "user123",
		Type:      "test",
		Message:   "Hello",
		CreatedAt: time.Now(),
	}

	ok := pool.Submit(task)
	assert.True(t, ok)

	time.Sleep(100 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := pool.Shutdown(shutdownCtx)
	assert.NoError(t, err)

	mockSender.AssertExpectations(t)
}

func TestPool_ShutdownWhenFull(t *testing.T) {
	mockSender := new(MockSender)
	ctx := context.Background()
	pool := worker.NewPool(ctx, mockSender, 1, 1)
	pool.Start()

	ok := pool.Submit(domain.NotificationTask{UserID: "u", Type: "t", Message: "m"})
	assert.True(t, ok)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := pool.Shutdown(shutdownCtx)
	assert.NoError(t, err)

	ok = pool.Submit(domain.NotificationTask{UserID: "u2", Type: "t2", Message: "m2"})
	assert.False(t, ok)
}
