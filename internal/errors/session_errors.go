package errors

import "errors"

var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrSessionExpired       = errors.New("session expired")
	ErrSessionAlreadyExists = errors.New("session already exists for this user and event")
	ErrInvalidSessionStatus = errors.New("invalid session status")
)
