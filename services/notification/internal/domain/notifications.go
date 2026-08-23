package domain

import "time"

// NotificationTask — задача на отправку уведомления пользователю.
type NotificationTask struct {
	UserID    string    `json:"user_id"`    // кому отправляем
	Type      string    `json:"type"`       // тип: workout_created, reminder, plan_generated
	Message   string    `json:"message"`    // текст уведомления
	CreatedAt time.Time `json:"created_at"` // когда задача создана
}
