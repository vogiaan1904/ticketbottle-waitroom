package service

import "errors"

var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrSessionExpired       = errors.New("session expired")
	ErrSessionAlreadyExists = errors.New("session already exists for this user and event")
	ErrInvalidSessionStatus = errors.New("invalid session status")

	ErrQueueFull           = errors.New("queue is full")
	ErrEventNotFound       = errors.New("event not found")
	ErrEventConfigNotFound = errors.New("event config not found")
	ErrWaitRoomNotAllowed  = errors.New("wait room is not allowed for this event")

	ErrProcessorStopped = errors.New("queue processor has been stopped")
	ErrEventNotActive   = errors.New("event is not active or not found")
)
