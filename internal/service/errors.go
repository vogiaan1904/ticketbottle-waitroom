package service

import "errors"

var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrSessionExpired       = errors.New("session expired")
	ErrSessionAlreadyExists = errors.New("session already exists for this user and event")
	ErrInvalidSessionStatus = errors.New("invalid session status")

	ErrQueueFull       = errors.New("queue is full")
	ErrEventNotFound   = errors.New("event not found")
	ErrQueueNotEnabled = errors.New("queue not enabled for this event")
)
