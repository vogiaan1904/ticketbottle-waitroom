package domain

import "time"

type JoinQueueRequest struct {
	UserID    string `json:"user_id" validate:"required"`
	EventID   string `json:"event_id" validate:"required"`
	Priority  int    `json:"priority" validate:"gte=0,lte=10"`
	UserAgent string `json:"-"`
	IPAddress string `json:"-"`
}

type JoinQueueResponse struct {
	SessionID        string    `json:"session_id"`
	Position         int64     `json:"position"`
	QueueLength      int64     `json:"queue_length"`
	EstimatedWaitSec int       `json:"estimated_wait_seconds"`
	QueuedAt         time.Time `json:"queued_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	WebSocketURL     string    `json:"websocket_url,omitempty"`
}

type QueueStatusResponse struct {
	SessionID         string        `json:"session_id"`
	Status            SessionStatus `json:"status"`
	Position          int64         `json:"position,omitempty"`
	QueueLength       int64         `json:"queue_length,omitempty"`
	EstimatedWaitSec  int           `json:"estimated_wait_seconds,omitempty"`
	QueuedAt          time.Time     `json:"queued_at"`
	ExpiresAt         time.Time     `json:"expires_at"`
	CheckoutToken     string        `json:"checkout_token,omitempty"`
	CheckoutURL       string        `json:"checkout_url,omitempty"`
	CheckoutExpiresAt *time.Time    `json:"checkout_expires_at,omitempty"`
	AdmittedAt        *time.Time    `json:"admitted_at,omitempty"`
}

type LeaveQueueResponse struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}
