package service

import (
	"context"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type QueueService interface {
	EnqueueSession(ctx context.Context, session *models.Session) (int64, error)
	DequeueSession(ctx context.Context, eventID, sessionID string) error
	GetQueueStatus(ctx context.Context, sessionID string, session *models.Session) (*QueueStatusOutput, error)
	GetQueueInfo(ctx context.Context, eventID string) (*QueueInfoOutput, error)
	RemoveFromProcessing(ctx context.Context, eventID, sessionID string) error
	GetProcessingCount(ctx context.Context, eventID string) (int64, error)
}

type queueService struct {
	repo repo.QueueRepository
	l    logger.Logger
}

func NewQueueService(
	repo repo.QueueRepository,
	l logger.Logger,
) QueueService {
	return &queueService{
		repo: repo,
		l:    l,
	}
}

func (s *queueService) EnqueueSession(ctx context.Context, ss *models.Session) (int64, error) {
	// Add to queue (Redis sorted set)
	if err := s.repo.AddToQueue(ctx, ss.EventID, ss); err != nil {
		return 0, fmt.Errorf("failed to add to queue: %w", err)
	}

	// Get position in queue
	pos, err := s.repo.GetQueuePosition(ctx, ss.EventID, ss.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to get queue position: %w", err)
	}

	// Update session object (caller will persist)
	ss.Position = pos

	s.l.Infof(ctx, "Session enqueued - id: %s, event_id: %s, position: %d", ss.ID, ss.EventID, pos)

	return pos, nil
}

func (s *queueService) DequeueSession(ctx context.Context, eventID, sessionID string) error {
	// Remove from queue
	if err := s.repo.RemoveFromQueue(ctx, eventID, sessionID); err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}

	s.l.Infof(ctx, "Session dequeued - session_id: %s, event_id: %s", sessionID, eventID)

	return nil
}

func (s *queueService) GetQueueStatus(ctx context.Context, sessionID string, session *models.Session) (*QueueStatusOutput, error) {
	out := &QueueStatusOutput{
		SessionID: session.ID,
		Status:    session.Status,
		QueuedAt:  session.QueuedAt,
		ExpiresAt: session.ExpiresAt,
	}

	// If still queued, get current position and queue info
	if session.Status == models.SessionStatusQueued {
		position, err := s.repo.GetQueuePosition(ctx, session.EventID, session.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get queue position: %w", err)
		}

		queueLength, err := s.repo.GetQueueLength(ctx, session.EventID)
		if err != nil {
			return nil, fmt.Errorf("failed to get queue length: %w", err)
		}

		out.Position = position
		out.QueueLength = queueLength
	}

	// If admitted, include checkout information
	if session.Status == models.SessionStatusAdmitted {
		out.CheckoutToken = session.CheckoutToken
		out.CheckoutExpiresAt = session.CheckoutExpiresAt
		out.AdmittedAt = session.AdmittedAt
		out.CheckoutURL = "/checkout" // This would be configurable
	}

	return out, nil
}

func (s *queueService) RemoveFromProcessing(ctx context.Context, eventID, sessionID string) error {
	if err := s.repo.RemoveFromProcessing(ctx, eventID, sessionID); err != nil {
		return fmt.Errorf("failed to remove from processing: %w", err)
	}
	return nil
}

func (s *queueService) GetProcessingCount(ctx context.Context, eventID string) (int64, error) {
	return s.repo.GetProcessingCount(ctx, eventID)
}

func (s *queueService) GetQueueInfo(ctx context.Context, eID string) (*QueueInfoOutput, error) {
	qLen, err := s.repo.GetQueueLength(ctx, eID)
	if err != nil {
		return nil, err
	}

	processingCount, err := s.repo.GetProcessingCount(ctx, eID)
	if err != nil {
		return nil, err
	}

	return &QueueInfoOutput{
		EventID:         eID,
		QueueLength:     qLen,
		ProcessingCount: processingCount,
	}, nil
}
