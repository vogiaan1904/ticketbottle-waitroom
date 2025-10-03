package service

import (
	"context"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type WaitroomService interface {
	JoinQueue(ctx context.Context, input JoinQueueInput) (JoinQueueOutput, error)
	GetQueueStatus(ctx context.Context, ssID string) (QueueStatusOutput, error)
	LeaveQueue(ctx context.Context, ssID string) error
}

type waitroomService struct {
	queueService   QueueService
	sessionService SessionService
	l              logger.Logger
}

func NewWaitroomService(
	queueService QueueService,
	sessionService SessionService,
	l logger.Logger,
) WaitroomService {
	return &waitroomService{
		queueService:   queueService,
		sessionService: sessionService,
		l:              l,
	}
}

func (s *waitroomService) JoinQueue(ctx context.Context, input JoinQueueInput) (JoinQueueOutput, error) {
	s.l.Info("Join queue request",
		"user_id", input.UserID,
		"event_id", input.EventID,
		"priority", input.Priority,
	)

	// Create session
	ss, err := s.sessionService.CreateSession(
		ctx,
		input.UserID,
		input.EventID,
		input.UserAgent,
		input.IPAddress,
	)
	if err != nil {
		return JoinQueueOutput{}, fmt.Errorf("failed to create session: %w", err)
	}

	// Enqueue session
	pos, err := s.queueService.EnqueueSession(ctx, ss)
	if err != nil {
		return JoinQueueOutput{}, fmt.Errorf("failed to enqueue session: %w", err)
	}

	// Get queue length
	qInf, err := s.queueService.GetQueueInfo(ctx, input.EventID)
	if err != nil {
		return JoinQueueOutput{}, fmt.Errorf("failed to get queue info: %w", err)
	}

	out := JoinQueueOutput{
		SessionID:    ss.ID,
		Position:     pos,
		QueueLength:  qInf.QueueLength,
		QueuedAt:     ss.QueuedAt,
		ExpiresAt:    ss.ExpiresAt,
		WebSocketURL: fmt.Sprintf("/api/v1/waitroom/stream/%s", ss.ID),
	}

	s.l.Info("User joined queue",
		"session_id", ss.ID,
		"user_id", input.UserID,
		"event_id", input.EventID,
		"position", pos,
	)

	return out, nil
}

func (s *waitroomService) GetQueueStatus(ctx context.Context, ssID string) (QueueStatusOutput, error) {
	// Validate session exists and is valid
	if err := s.sessionService.ValidateSession(ctx, ssID); err != nil {
		return QueueStatusOutput{}, err
	}

	// Get queue status
	status, err := s.queueService.GetQueueStatus(ctx, ssID)
	if err != nil {
		return QueueStatusOutput{}, fmt.Errorf("failed to get queue status: %w", err)
	}

	return status, nil
}

func (s *waitroomService) LeaveQueue(ctx context.Context, ssID string) error {
	s.l.Info("Leave queue request", "session_id", ssID)

	// Get session to get event ID
	ss, err := s.sessionService.GetSession(ctx, ssID)
	if err != nil {
		return err
	}

	// Remove from queue
	if err := s.queueService.DequeueSession(ctx, ss.EventID, ssID); err != nil {
		return fmt.Errorf("failed to leave queue: %w", err)
	}

	s.l.Info("User left queue",
		"session_id", ssID,
		"user_id", ss.UserID,
		"event_id", ss.EventID,
	)

	return nil
}
