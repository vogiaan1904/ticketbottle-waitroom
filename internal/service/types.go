package service

import (
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
)

type JoinQueueInput struct {
	UserID    string `json:"user_id" validate:"required"`
	EventID   string `json:"event_id" validate:"required"`
	Priority  int    `json:"priority" validate:"gte=0,lte=10"`
	UserAgent string `json:"-"`
	IPAddress string `json:"-"`
}

type JoinQueueOutput struct {
	SessionID    string    `json:"session_id"`
	Position     int64     `json:"position"`
	QueueLength  int64     `json:"queue_length"`
	QueuedAt     time.Time `json:"queued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	WebSocketURL string    `json:"websocket_url,omitempty"`
}

type QueueStatusOutput struct {
	SessionID         string               `json:"session_id"`
	Status            models.SessionStatus `json:"status"`
	Position          int64                `json:"position,omitempty"`
	QueueLength       int64                `json:"queue_length,omitempty"`
	QueuedAt          time.Time            `json:"queued_at"`
	ExpiresAt         time.Time            `json:"expires_at"`
	CheckoutToken     string               `json:"checkout_token,omitempty"`
	CheckoutURL       string               `json:"checkout_url,omitempty"`
	CheckoutExpiresAt *time.Time           `json:"checkout_expires_at,omitempty"`
	AdmittedAt        *time.Time           `json:"admitted_at,omitempty"`
}

type LeaveQueueOutput struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}
