package service

import (
	"context"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	pkgLog "github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/event"
)

type WaitroomService interface {
	JoinQueue(ctx context.Context, req *JoinQueueInput) (*JoinQueueOutput, error)
	GetQueueStatus(ctx context.Context, ssID string) (*QueueStatusOutput, error)
	LeaveQueue(ctx context.Context, ssID string) error
	HandleCheckoutCompleted(ctx context.Context, in CheckoutCompletedInput) error
	HandleCheckoutFailed(ctx context.Context, in CheckoutFailedInput) error
	HandleCheckoutExpired(ctx context.Context, in CheckoutExpiredInput) error

	// Queue processor methods
	StartQueueProcessor(ctx context.Context) error
	StopQueueProcessor() error
	GetProcessorStatus() ProcessorStatus

	// Real-time position streaming
	StreamSessionPosition(ctx context.Context, sessionID string, updates chan<- *PositionStreamUpdate) error
}

type waitroomService struct {
	qSvc  QueueService
	ssSvc SessionService
	eSvc  event.EventServiceClient
	prod  producer.Producer
	l     pkgLog.Logger
	proc  QueueProcessor
}

func NewWaitroomService(
	qSvc QueueService,
	ssSvc SessionService,
	eSvc event.EventServiceClient,
	prod producer.Producer,
	l pkgLog.Logger,
	proc QueueProcessor,
) WaitroomService {
	return &waitroomService{
		qSvc:  qSvc,
		ssSvc: ssSvc,
		eSvc:  eSvc,
		prod:  prod,
		l:     l,
		proc:  proc,
	}
}

func (s *waitroomService) JoinQueue(ctx context.Context, in *JoinQueueInput) (*JoinQueueOutput, error) {
	out, err := s.eSvc.FindOne(ctx, &event.FindOneEventRequest{
		Id: in.EventID,
	})
	if err != nil {
		s.l.Errorf(ctx, "service.waitroomService.JoinQueue: %v", err)
		return nil, err
	}

	if out.Event == nil {
		s.l.Warnf(ctx, "service.waitroomService.JoinQueue: %v", ErrEventNotFound)
		return nil, ErrEventNotFound
	}

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
	if err := s.ssSvc.ValidateSession(ctx, ssID); err != nil {
		return nil, err
	}

	ss, err := s.ssSvc.GetSession(ctx, ssID)
	if err != nil {
		return nil, err
	}

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

func (s *waitroomService) StartQueueProcessor(ctx context.Context) error {
	if s.proc == nil {
		return fmt.Errorf("queue processor not initialized")
	}
	return s.proc.Start(ctx)
}

func (s *waitroomService) StopQueueProcessor() error {
	if s.proc == nil {
		return fmt.Errorf("queue processor not initialized")
	}
	return s.proc.Stop()
}

func (s *waitroomService) GetProcessorStatus() ProcessorStatus {
	if s.proc == nil {
		return ProcessorStatus{IsRunning: false}
	}
	return s.proc.GetStatus()
}

func (s *waitroomService) StreamSessionPosition(ctx context.Context, ssID string, upds chan<- *PositionStreamUpdate) error {
	if err := s.ssSvc.ValidateSession(ctx, ssID); err != nil {
		return err
	}

	ss, err := s.ssSvc.GetSession(ctx, ssID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Send initial position update immediately
	initUpd, err := s.buildPositionUpdate(ctx, ss)
	if err != nil {
		return fmt.Errorf("failed to build initial position update: %w", err)
	}

	select {
	case upds <- initUpd:
		s.l.Debug(ctx, "Sent initial position update",
			"session_id", ssID,
			"position", initUpd.Position,
		)
	case <-ctx.Done():
		return ctx.Err()
	}

	// Subscribe to Redis Pub/Sub for position updates on this event
	sub, err := s.qSvc.SubscribeToPositionUpdates(ctx, ss.EventID)
	if err != nil {
		return fmt.Errorf("failed to subscribe to position updates: %w", err)
	}
	defer sub.Close()

	s.l.Info(ctx, "Started streaming position updates",
		"session_id", ssID,
		"event_id", ss.EventID,
	)

	// Listen for updates on the channel
	updCh := sub.Updates()
	for {
		select {
		case <-ctx.Done():
			s.l.Info(ctx, "Position stream closed by context",
				"session_id", ssID,
			)
			return ctx.Err()

		case event := <-updCh:
			if event == nil {
				// Channel closed
				return nil
			}

			s.l.Debug(ctx, "Received position update event",
				"session_id", ssID,
				"update_type", event.UpdateType,
			)

			// Get updated session data
			ss, err = s.ssSvc.GetSession(ctx, ssID)
			if err != nil {
				s.l.Errorf(ctx, "Failed to get session during stream: %v", err)
				continue
			}

			// Build and send position update
			upd, err := s.buildPositionUpdate(ctx, ss)
			if err != nil {
				s.l.Errorf(ctx, "Failed to build position update: %v", err)
				continue
			}

			select {
			case upds <- upd:
				s.l.Debug(ctx, "Sent position update",
					"session_id", ssID,
					"position", upd.Position,
					"status", upd.Status,
				)
			case <-ctx.Done():
				return ctx.Err()
			}

			// If session is admitted, completed, failed, or expired, close the stream
			if ss.Status != models.SessionStatusQueued {
				s.l.Info(ctx, "Session status changed, closing stream",
					"session_id", ssID,
					"status", ss.Status,
				)
				return nil
			}
		}
	}
}

func (s *waitroomService) buildPositionUpdate(ctx context.Context, ss *models.Session) (*PositionStreamUpdate, error) {
	upd := &PositionStreamUpdate{
		SessionID: ss.ID,
		Status:    ss.Status,
		UpdatedAt: ss.UpdatedAt,
	}

	// If session is queued, get current position
	if ss.Status == models.SessionStatusQueued {
		status, err := s.qSvc.GetQueueStatus(ctx, ss.ID, ss)
		if err != nil {
			return nil, fmt.Errorf("failed to get queue status: %w", err)
		}
		upd.Position = status.Position
		upd.QueueLength = status.QueueLength
	}

	// If session is admitted, include checkout information
	if ss.Status == models.SessionStatusAdmitted {
		upd.CheckoutToken = ss.CheckoutToken
		upd.CheckoutURL = "/checkout" // This would be configurable
		upd.CheckoutExpiresAt = ss.CheckoutExpiresAt
	}

	return upd, nil
}
