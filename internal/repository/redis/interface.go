package repository

import (
	"context"
	"time"
)

type RedisRepository interface {
	// Queue operations
	AddToQueue(ctx context.Context, eventID string, session Session) error
	RemoveFromQueue(ctx context.Context, eventID, sessionID string) error
	PopFromQueue(ctx context.Context, eventID string, count int) ([]string, error)
	GetQueueLength(ctx context.Context, eventID string) (int64, error)
	GetQueuePosition(ctx context.Context, eventID, sessionID string) (int64, error)

	// Session operations
	SaveSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, sessionID string) (*Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	UpdateSessionStatus(ctx context.Context, sessionID, status string) error

	// Processing set
	AddToProcessing(ctx context.Context, eventID, sessionID string, ttl time.Duration) error
	RemoveFromProcessing(ctx context.Context, eventID, sessionID string) error
	IsProcessing(ctx context.Context, eventID, sessionID string) (bool, error)
	GetProcessingCount(ctx context.Context, eventID string) (int64, error)

	// Event config
	GetEventConfig(ctx context.Context, eventID string) (*EventConfig, error)
	SetEventConfig(ctx context.Context, config EventConfig) error

	// Distributed lock
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key string) error
}
