package errors

import "errors"

var (
	ErrQueueFull       = errors.New("queue is full")
	ErrEventNotFound   = errors.New("event not found")
	ErrQueueNotEnabled = errors.New("queue not enabled for this event")
)
