package worker

import (
	"context"
	"sync"
	"time"

	"fitness-platform/pkg/logger"
	"fitness-platform/services/notification/internal/domain"
	"fitness-platform/services/notification/internal/sender"
)

// Pool хранит reminder-задачи в памяти.
// Важно: RAM-очередь не является долговременным хранилищем.
type Pool struct {
	taskCh  chan domain.NotificationTask
	sender  sender.Sender
	workers int
	wg      sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc

	mu           sync.Mutex
	stopped      bool
	stopCh       chan struct{}
	submitWG     sync.WaitGroup
	shutdownOnce sync.Once
	shutdownDone chan struct{}
}

func NewPool(
	ctx context.Context,
	notificationSender sender.Sender,
	workers int,
	bufferSize int,
) *Pool {
	poolCtx, cancel := context.WithCancel(ctx)

	return &Pool{
		taskCh:       make(chan domain.NotificationTask, bufferSize),
		sender:       notificationSender,
		workers:      workers,
		ctx:          poolCtx,
		cancel:       cancel,
		stopCh:       make(chan struct{}),
		shutdownDone: make(chan struct{}),
	}
}

// Start вызывается один раз до Shutdown.
func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)

		go func() {
			defer p.wg.Done()
			p.workerLoop()
		}()
	}
}

// Submit регистрирует активную отправку в канал.
// Благодаря submitWG Shutdown не закроет taskCh
// одновременно с записью в него.
func (p *Pool) Submit(
	task domain.NotificationTask,
) bool {
	p.mu.Lock()

	if p.stopped {
		p.mu.Unlock()
		return false
	}

	p.submitWG.Add(1)
	p.mu.Unlock()

	defer p.submitWG.Done()

	select {
	case <-p.ctx.Done():
		return false

	case <-p.stopCh:
		return false

	case p.taskCh <- task:
		return true
	}
}

// Shutdown останавливает приём новых задач,
// дожидается завершения уже принятых задач
// и отменяет работников при deadline.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		p.mu.Lock()
		p.stopped = true
		close(p.stopCh)
		p.mu.Unlock()

		go func() {
			// Ждём уже начатые вызовы Submit().
			p.submitWG.Wait()

			// После этого отправителей в taskCh
			// больше нет — его безопасно закрыть.
			close(p.taskCh)

			// Ждём, пока все workerLoop закончатся.
			p.wg.Wait()

			close(p.shutdownDone)
		}()
	})

	select {
	case <-p.shutdownDone:
		p.cancel()
		return nil

	case <-ctx.Done():
		p.cancel()
		return ctx.Err()
	}
}

func (p *Pool) workerLoop() {
	for task := range p.taskCh {
		if p.ctx.Err() != nil {
			return
		}

		p.processTask(task)
	}
}

func (p *Pool) processTask(
	task domain.NotificationTask,
) {
	ctx, cancel := context.WithTimeout(
		p.ctx,
		10*time.Second,
	)
	defer cancel()

	if err := p.sender.Send(
		ctx,
		task.UserID,
		task.Message,
	); err != nil {
		logger.Log.Error().
			Err(err).
			Str("user_id", task.UserID).
			Str("type", task.Type).
			Msg("Failed to send notification")
	}
}
