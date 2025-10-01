package service

import (
	"context"

	"github.com/yourusername/waitroom-service/internal/domain"
)

type WaitroomService interface {
	JoinQueue(ctx context.Context, req JoinRequest) (*Session, error)
	GetQueueStatus(ctx context.Context, sessionID string) (*QueueStatus, error)
	LeaveQueue(ctx context.Context, sessionID string) error
	ProcessQueue(ctx context.Context, eventID string) error
}

type QueueService interface {
	Enqueue(ctx context.Context, session Session) (position int64, err error)
	Dequeue(ctx context.Context, eventID string, count int) ([]Session, error)
	GetPosition(ctx context.Context, sessionID string) (int64, error)
	Remove(ctx context.Context, sessionID string) error
	GetQueueLength(ctx context.Context, eventID string) (int64, error)
}

type SessionService interface {
	Create(ctx context.Context, userID, eventID string, priority int, userAgent, ipAddress string) (*domain.Session, error)
	Get(ctx context.Context, sessionID string) (*domain.Session, error)
	GenerateCheckoutToken(ctx context.Context, session *domain.Session) (string, error)
	Validate(ctx context.Context, sessionID string) error
}
