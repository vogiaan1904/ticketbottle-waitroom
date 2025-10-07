package service

import (
	"context"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	pkgLog "github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type WaitroomService interface {
	JoinQueue(ctx context.Context, req *JoinQueueInput) (*JoinQueueOutput, error)
	GetQueueStatus(ctx context.Context, ssID string) (*QueueStatusOutput, error)
	LeaveQueue(ctx context.Context, ssID string) error
	HandleCheckoutCompleted(ctx context.Context, in CheckoutCompletedInput) error
	HandleCheckoutFailed(ctx context.Context, in CheckoutFailedInput) error
	HandleCheckoutExpired(ctx context.Context, in CheckoutExpiredInput) error
}

type waitroomService struct {
	qSvc  QueueService
	ssSvc SessionService
	prod  producer.Producer
	l     pkgLog.Logger
}

func NewWaitroomService(
	qSvc QueueService,
	ssSvc SessionService,
	prod producer.Producer,
	l pkgLog.Logger,
) WaitroomService {
	return &waitroomService{
		qSvc:  qSvc,
		ssSvc: ssSvc,
		prod:  prod,
		l:     l,
	}
}

func (s *waitroomService) JoinQueue(ctx context.Context, in *JoinQueueInput) (*JoinQueueOutput, error) {
	ss, err := s.ssSvc.CreateSession(
		ctx,
		in.UserID,
		in.EventID,
		in.UserAgent,
		in.IPAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	pos, err := s.qSvc.EnqueueSession(ctx, ss)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue session: %w", err)
	}

	if err := s.prod.PublishQueueJoined(ctx, kafka.QueueJoinedEvent{
		SessionID: ss.ID,
		UserID:    ss.UserID,
		EventID:   ss.EventID,
		Position:  pos,
		JoinedAt:  ss.QueuedAt,
	}); err != nil {
		s.l.Errorf(ctx, "service.waitroomService.JoinQueue: %v", err)
	}

	if err := s.ssSvc.UpdateSession(ctx, ss); err != nil {
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	qInf, err := s.qSvc.GetQueueInfo(ctx, in.EventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue info: %w", err)
	}

	return &JoinQueueOutput{
		SessionID:    ss.ID,
		Position:     pos,
		QueueLength:  qInf.QueueLength,
		QueuedAt:     ss.QueuedAt,
		ExpiresAt:    ss.ExpiresAt,
		WebSocketURL: fmt.Sprintf("/api/v1/waitroom/stream/%s", ss.ID),
	}, nil
}

func (s *waitroomService) GetQueueStatus(ctx context.Context, ssID string) (*QueueStatusOutput, error) {
	// Step 1: Validate and get session (SessionService)
	if err := s.ssSvc.ValidateSession(ctx, ssID); err != nil {
		return nil, err
	}

	ss, err := s.ssSvc.GetSession(ctx, ssID)
	if err != nil {
		return nil, err
	}

	// Step 2: Get queue status (qSvc uses session data)
	stt, err := s.qSvc.GetQueueStatus(ctx, ssID, ss)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue status: %w", err)
	}

	return stt, nil
}

func (s *waitroomService) LeaveQueue(ctx context.Context, ssID string) error {
	ss, err := s.ssSvc.GetSession(ctx, ssID)
	if err != nil {
		s.l.Errorf(ctx, "waitroomService.LeaveQueue: %v", err)
		return err
	}

	if err := s.qSvc.DequeueSession(ctx, ss.EventID, ssID); err != nil {
		return fmt.Errorf("failed to leave queue: %w", err)
	}

	if err := s.ssSvc.UpdateSessionStatus(ctx, ssID, models.SessionStatusAbandoned); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	if err := s.prod.PublishQueueLeft(ctx, kafka.QueueLeftEvent{
		SessionID: ssID,
		UserID:    ss.UserID,
		EventID:   ss.EventID,
		Reason:    "user_left",
		LeftAt:    ss.UpdatedAt,
	}); err != nil {
		s.l.Error(ctx, "Failed to publish queue left event",
			"session_id", ssID,
			"error", err,
		)
	}

	s.l.Info(ctx, "User left queue",
		"session_id", ssID,
		"user_id", ss.UserID,
		"event_id", ss.EventID,
	)

	return nil
}

func (s *waitroomService) HandleCheckoutCompleted(ctx context.Context, in CheckoutCompletedInput) error {
	ss, err := s.ssSvc.GetSession(ctx, in.SessionID)
	if err != nil {
		if err == ErrSessionNotFound {
			s.l.Warnf(ctx, "Session not found for completed checkout",
				"session_id", in.SessionID,
			)
			return nil
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	if err := s.qSvc.RemoveFromProcessing(ctx, in.EventID, in.SessionID); err != nil {
		s.l.Errorf(ctx, "Failed to remove from processing",
			"session_id", in.SessionID,
			"error", err,
		)
	}

	ss.Status = models.SessionStatusCompleted
	now := in.Timestamp
	ss.CompletedAt = &now

	if err := s.ssSvc.UpdateSession(ctx, ss); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	s.l.Info(ctx, "Checkout completed event processed",
		"session_id", in.SessionID,
	)

	return nil
}

func (s *waitroomService) HandleCheckoutFailed(ctx context.Context, in CheckoutFailedInput) error {
	ss, err := s.ssSvc.GetSession(ctx, in.SessionID)
	if err != nil {
		if err == ErrSessionNotFound {
			s.l.Warnf(ctx, "Session not found for failed checkout",
				"session_id", in.SessionID,
			)
			return nil
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Step 2: Remove from processing (qSvc)
	if err := s.qSvc.RemoveFromProcessing(ctx, in.EventID, in.SessionID); err != nil {
		s.l.Errorf(ctx, "Failed to remove from processing",
			"session_id", in.SessionID,
			"error", err,
		)
	}

	ss.Status = models.SessionStatusFailed
	if err := s.ssSvc.UpdateSession(ctx, ss); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	s.l.Info(ctx, "Checkout failed event processed",
		"session_id", in.SessionID,
	)

	return nil
}

func (s *waitroomService) HandleCheckoutExpired(ctx context.Context, in CheckoutExpiredInput) error {
	ss, err := s.ssSvc.GetSession(ctx, in.SessionID)
	if err != nil {
		if err == ErrSessionNotFound {
			s.l.Warnf(ctx, "Session not found for expired checkout",
				"session_id", in.SessionID,
			)
			return nil
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	if err := s.qSvc.RemoveFromProcessing(ctx, in.EventID, in.SessionID); err != nil {
		s.l.Errorf(ctx, "Failed to remove from processing",
			"session_id", in.SessionID,
			"error", err,
		)
	}

	ss.Status = models.SessionStatusExpired
	if err := s.ssSvc.UpdateSession(ctx, ss); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	s.l.Info(ctx, "Checkout expired event processed",
		"session_id", in.SessionID,
	)

	return nil
}
