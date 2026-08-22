package worker

import (
	"context"
	"sync"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/notification/internal/domain"
	"fitness-platform/services/notification/internal/sender"
)

// Pool — пул воркеров для обработки уведомлений.
type Pool struct {
	taskCh  chan domain.NotificationTask
	sender  sender.Sender
	workers int
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewPool создаёт новый пул.
// Принимает родительский контекст, отправителя, число воркеров и размер буфера канала.
func NewPool(ctx context.Context, sender sender.Sender, workers int, bufferSize int) *Pool {
	poolCtx, cancel := context.WithCancel(ctx)
	return &Pool{
		taskCh:  make(chan domain.NotificationTask, bufferSize),
		sender:  sender,
		workers: workers,
		ctx:     poolCtx,
		cancel:  cancel,
	}
}

// Start запускает воркеров.
func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.workerLoop()
		}()
	}
}

// Submit добавляет задачу в канал.
// Возвращает true, если задача была принята, и false, если пул остановлен.
func (p *Pool) Submit(task domain.NotificationTask) bool {
	select {
	case <-p.ctx.Done():
		return false
	case p.taskCh <- task:
		return true
	}
}

// Shutdown останавливает пул и ожидает завершения воркеров.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.cancel()
	close(p.taskCh)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// workerLoop — цикл обработки одного воркера.
func (p *Pool) workerLoop() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case task, ok := <-p.taskCh:
			if !ok {
				return // канал закрыт
			}
			p.processTask(task)
		}
	}
}

// processTask отправляет уведомление с таймаутом.
func (p *Pool) processTask(task domain.NotificationTask) {
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	if err := p.sender.Send(ctx, task.UserID, task.Message); err != nil {
		logger.Log.Error().
			Err(err).
			Str("user_id", task.UserID).
			Str("type", task.Type).
			Msg("Failed to send notification")
	}
}
