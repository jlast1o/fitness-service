package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/notification/internal/domain"
	"fitness-platform/services/notification/internal/worker"
)

// ReminderPoller периодически опрашивает Planner Service для получения напоминаний.
type ReminderPoller struct {
	plannerBaseURL string
	pool           *worker.Pool
	interval       time.Duration
	client         *http.Client
	internalToken  string
}

// NewReminderPoller создаёт новый poller.
func NewReminderPoller(plannerBaseURL string, pool *worker.Pool, interval time.Duration, internalToken string) *ReminderPoller {
	return &ReminderPoller{
		plannerBaseURL: plannerBaseURL,
		pool:           pool,
		interval:       interval,
		internalToken:  internalToken,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Run запускает цикл опроса.
func (p *ReminderPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info().Msg("reminder poller stopped")
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

// poll выполняет один запрос к Planner и ставит задачи в пул.
func (p *ReminderPoller) poll(ctx context.Context) {
	url := fmt.Sprintf("%s/internal/reminders?window=24h", p.plannerBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to create reminder request")
		return
	}
	// Внутренний токен, чтобы обычные пользователи не могли вызвать этот эндпоинт
	req.Header.Set("X-Internal-Token", p.internalToken)

	resp, err := p.client.Do(req)
	if err != nil {
		logger.Log.Error().Err(err).Msg("failed to poll planner for reminders")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Log.Warn().Int("status", resp.StatusCode).Msg("unexpected status from planner")
		return
	}

	var reminders []struct {
		UserID  string `json:"user_id"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reminders); err != nil {
		logger.Log.Error().Err(err).Msg("failed to decode reminders")
		return
	}

	for _, r := range reminders {
		task := domain.NotificationTask{
			UserID:  r.UserID,
			Type:    "reminder",
			Message: r.Message,
		}
		if !p.pool.Submit(task) {
			// Пул остановлен, прекращаем опрос
			return
		}
	}
}
