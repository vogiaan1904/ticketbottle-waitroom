package grpc

import (
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	pkgErrors "github.com/vogiaan1904/ticketbottle-waitroom/pkg/errors"
)

var (
	errSessionNotFound      = pkgErrors.NewGRPCError("WTR001", "Session not found")
	errSessionExpired       = pkgErrors.NewGRPCError("WTR002", "Session expired")
	errSessionAlreadyExists = pkgErrors.NewGRPCError("WTR003", "Session already exists")
	errInvalidSessionStatus = pkgErrors.NewGRPCError("WTR004", "Invalid session status")

	errQueueFull       = pkgErrors.NewGRPCError("WTR005", "Queue is full")
	errEventNotFound   = pkgErrors.NewGRPCError("WTR006", "Event not found")
	errQueueNotEnabled = pkgErrors.NewGRPCError("WTR007", "Queue is not enabled")
)

func (svc *WaitroomGrpcService) mapGRPCError(err error) error {
	switch err {
	case service.ErrSessionNotFound:
		return errSessionNotFound
	case service.ErrSessionExpired:
		return errSessionExpired
	case service.ErrSessionAlreadyExists:
		return errSessionAlreadyExists
	case service.ErrInvalidSessionStatus:
		return errInvalidSessionStatus
	case service.ErrQueueFull:
		return errQueueFull
	case service.ErrEventNotFound:
		return errEventNotFound
	case service.ErrQueueNotEnabled:
		return errQueueNotEnabled
	default:
		return err
	}
}
