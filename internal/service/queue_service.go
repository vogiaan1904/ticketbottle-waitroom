package service

import (
	"context"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/queue"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type QueueService interface {
	EnqueueSession(ctx context.Context, ss *models.Session) (int64, error)
	DequeueSession(ctx context.Context, eID, ssID string) error
	GetQueueStatus(ctx context.Context, ssID string) (QueueStatusOutput, error)
	GetQueueInfo(ctx context.Context, eID string) (*queue.QueueInfo, error)
}

type queueService struct {
	queueRepo    repo.QueueRepository
	sessionRepo  repo.SessionRepository
	queueManager *queue.Manager
	prod         kafka.Producer
	logger       logger.Logger
}

func NewQueueService(
	queueRepo repo.QueueRepository,
	sessionRepo repo.SessionRepository,
	queueManager *queue.Manager,
	prod kafka.Producer,
	logger logger.Logger,
) QueueService {
	return &queueService{
		queueRepo:    queueRepo,
		sessionRepo:  sessionRepo,
		queueManager: queueManager,
		prod:         prod,
		logger:       logger,
	}
}

func (s *queueService) EnqueueSession(ctx context.Context, ss *models.Session) (int64, error) {
	// Add to queue (Redis sorted set)
	if err := s.queueRepo.AddToQueue(ctx, ss.EventID, ss); err != nil {
		return 0, fmt.Errorf("failed to add to queue: %w", err)
	}

	pos, err := s.queueRepo.GetQueuePosition(ctx, ss.EventID, ss.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to get queue position: %w", err)
	}

	ss.Position = pos
	if err := s.sessionRepo.Update(ctx, ss); err != nil {
		return 0, fmt.Errorf("failed to update session: %w", err)
	}

	if s.prod != nil {
		if err := s.prod.PublishQueueJoined(ctx, kafka.QueueJoinedEvent{
			SessionID: ss.ID,
			UserID:    ss.UserID,
			EventID:   ss.EventID,
			Position:  pos,
			JoinedAt:  ss.QueuedAt,
		}); err != nil {
			// Log error but don't fail the request
			s.logger.Error("Failed to publish queue joined event",
				"session_id", ss.ID,
				"error", err,
			)
		}
	}

	s.logger.Info("Session enqueued",
		"session_id", ss.ID,
		"event_id", ss.EventID,
		"position", pos,
	)

	return pos, nil
}

func (s *queueService) DequeueSession(ctx context.Context, eID, ssID string) error {
	// Get session info before removing
	ss, err := s.sessionRepo.Get(ctx, ssID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Remove from queue
	if err := s.queueRepo.RemoveFromQueue(ctx, eID, ssID); err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}

	// Update session status
	if err := s.sessionRepo.UpdateStatus(ctx, ssID, models.SessionStatusAbandoned); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	// Publish QueueLeft event to Kafka
	if s.prod != nil {
		if err := s.prod.PublishQueueLeft(ctx, kafka.QueueLeftEvent{
			SessionID: ssID,
			UserID:    ss.UserID,
			EventID:   eID,
			Reason:    "user_left",
			LeftAt:    ss.UpdatedAt,
		}); err != nil {
			s.logger.Error("Failed to publish queue left event",
				"session_id", ssID,
				"error", err,
			)
		}
	}

	s.logger.Info("Session dequeued",
		"session_id", ssID,
		"event_id", eID,
	)

	return nil
}

func (s *queueService) GetQueueStatus(ctx context.Context, ssID string) (QueueStatusOutput, error) {
	ss, err := s.sessionRepo.Get(ctx, ssID)
	if err != nil {
		return QueueStatusOutput{}, err
	}

	resp := QueueStatusOutput{
		SessionID: ss.ID,
		Status:    ss.Status,
		QueuedAt:  ss.QueuedAt,
		ExpiresAt: ss.ExpiresAt,
	}

	// If still queued, get current position and queue info
	if ss.Status == models.SessionStatusQueued {
		position, err := s.queueRepo.GetQueuePosition(ctx, ss.EventID, ss.ID)
		if err != nil {
			return QueueStatusOutput{}, fmt.Errorf("failed to get queue position: %w", err)
		}

		qLen, err := s.queueRepo.GetQueueLength(ctx, ss.EventID)
		if err != nil {
			return QueueStatusOutput{}, fmt.Errorf("failed to get queue length: %w", err)
		}

		resp.Position = position
		resp.QueueLength = qLen
	}

	// If admitted, include checkout information
	if ss.Status == models.SessionStatusAdmitted {
		resp.CheckoutToken = ss.CheckoutToken
		resp.CheckoutExpiresAt = ss.CheckoutExpiresAt
		resp.AdmittedAt = ss.AdmittedAt
		resp.CheckoutURL = "/checkout" // This would be configurable
	}

	return resp, nil
}

func (s *queueService) GetQueueInfo(ctx context.Context, eID string) (*queue.QueueInfo, error) {
	return s.queueManager.GetQueueInfo(ctx, eID)
}
