package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/errors"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type Manager struct {
	queueRepo   repo.QueueRepository
	sessionRepo repo.SessionRepository
	config      config.QueueConfig
	logger      logger.Logger
}

func NewManager(
	queueRepo repo.QueueRepository,
	sessionRepo repo.SessionRepository,
	config config.QueueConfig,
	logger logger.Logger,
) *Manager {
	return &Manager{
		queueRepo:   queueRepo,
		sessionRepo: sessionRepo,
		config:      config,
		logger:      logger,
	}
}

// GetQueueInfo returns current queue information for an event
func (m *Manager) GetQueueInfo(ctx context.Context, eventID string) (*QueueInfo, error) {
	queueLength, err := m.queueRepo.GetQueueLength(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue length: %w", err)
	}

	processingCount, err := m.queueRepo.GetProcessingCount(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get processing count: %w", err)
	}

	return &QueueInfo{
		EventID:         eventID,
		QueueLength:     queueLength,
		ProcessingCount: processingCount,
		MaxConcurrent:   int64(m.config.DefaultMaxConcurrent),
		ReleaseRate:     int64(m.config.DefaultReleaseRate),
	}, nil
}

// CanJoinQueue checks if a user can join the queue
func (m *Manager) CanJoinQueue(ctx context.Context, userID, eventID string) error {
	// Check if user already has a session for this event
	existingSession, err := m.sessionRepo.GetByUserAndEvent(ctx, userID, eventID)
	if err != nil && err != errors.ErrSessionNotFound {
		return fmt.Errorf("failed to check existing session: %w", err)
	}

	if existingSession != nil && existingSession.IsActive() {
		return errors.ErrSessionAlreadyExists
	}

	// Additional checks can be added here:
	// - Event capacity
	// - Queue enabled status
	// - Rate limiting
	// - User permissions

	return nil
}

// UpdateQueuePositions updates positions for all sessions in a queue
// This should be called periodically by a background job
func (m *Manager) UpdateQueuePositions(ctx context.Context, eventID string) error {
	qlen, err := m.queueRepo.GetQueueLength(ctx, eventID)
	if err != nil {
		return fmt.Errorf("failed to get queue length: %w", err)
	}

	if qlen == 0 {
		return nil
	}

	// Get all members in queue
	ssIDs, err := m.queueRepo.GetQueueMembers(ctx, eventID, 0, -1)
	if err != nil {
		return fmt.Errorf("failed to get queue members: %w", err)
	}

	// Update each session's position
	for i, ssID := range ssIDs {
		pos := int64(i + 1)

		// Get session
		ss, err := m.sessionRepo.Get(ctx, ssID)
		if err != nil {
			m.logger.Warn("Failed to get session for position update",
				"session_id", ssID,
				"error", err,
			)
			continue
		}

		// Update position only
		ss.Position = pos
		ss.UpdatedAt = time.Now()

		if err := m.sessionRepo.Update(ctx, ss); err != nil {
			m.logger.Warn("Failed to update session position",
				"session_id", ssID,
				"error", err,
			)
		}
	}

	m.logger.Debug("Updated queue positions",
		"event_id", eventID,
		"queue_length", len(ssIDs),
	)

	return nil
}

type QueueInfo struct {
	EventID         string `json:"event_id"`
	QueueLength     int64  `json:"queue_length"`
	ProcessingCount int64  `json:"processing_count"`
	MaxConcurrent   int64  `json:"max_concurrent"`
	ReleaseRate     int64  `json:"release_rate"`
}
