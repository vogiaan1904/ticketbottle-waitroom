package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/protogen/event"
)

var ()

type QueueProcessor interface {
	Start(ctx context.Context) error
	Stop() error
	ProcessEventQueue(ctx context.Context, eventID string) error
	GetStatus() ProcessorStatus
}

type ProcessorStatus struct {
	IsRunning     bool      `json:"is_running"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	LastProcessed time.Time `json:"last_processed,omitempty"`
	EventsActive  int       `json:"events_active"`
	TotalAdmitted int64     `json:"total_admitted"`
	ErrorCount    int64     `json:"error_count"`
}

type queueProcessor struct {
	// Dependencies
	queueSvc   QueueService
	sessionSvc SessionService
	eventSvc   event.EventServiceClient
	producer   producer.Producer
	logger     logger.Logger

	// Configuration
	config ProcessorConfig

	// State management
	mu        sync.RWMutex
	isRunning bool
	startedAt time.Time
	stopCh    chan struct{}
	ticker    *time.Ticker
	wg        sync.WaitGroup

	// Metrics
	lastProcessed time.Time
	totalAdmitted int64
	errorCount    int64
}

type ProcessorConfig struct {
	ProcessInterval       time.Duration // How often to process queues
	MaxConcurrentPerEvent int           // Max users in checkout per event
	BatchSize             int           // Max users to admit per batch
	EventCacheTTL         time.Duration // How long to cache active events
	RetryAttempts         int           // Retry attempts for failed operations
	RetryDelay            time.Duration // Delay between retries
	ShutdownTimeout       time.Duration // Max time to wait for graceful shutdown
	EnableMetrics         bool          // Enable detailed metrics collection
	MaxProcessingDuration time.Duration // Max time for processing all events
}

func NewQueueProcessor(
	queueSvc QueueService,
	sessionSvc SessionService,
	eventSvc event.EventServiceClient,
	producer producer.Producer,
	logger logger.Logger,
	cfg config.QueueConfig,
) QueueProcessor {
	return &queueProcessor{
		queueSvc:   queueSvc,
		sessionSvc: sessionSvc,
		eventSvc:   eventSvc,
		producer:   producer,
		logger:     logger,
		config: ProcessorConfig{
			ProcessInterval:       cfg.ProcessInterval,
			MaxConcurrentPerEvent: cfg.DefaultMaxConcurrent,
			BatchSize:             cfg.DefaultReleaseRate,
			EventCacheTTL:         5 * time.Minute,
			RetryAttempts:         3,
			RetryDelay:            time.Second,
			ShutdownTimeout:       30 * time.Second,
			EnableMetrics:         true,
			MaxProcessingDuration: 30 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

func (qp *queueProcessor) Start(ctx context.Context) error {
	qp.mu.Lock()
	defer qp.mu.Unlock()

	if qp.isRunning {
		return errors.New("queue processor is already running")
	}

	qp.logger.Infof(ctx, "Starting queue processor - interval: %v, max_concurrent: %d, batch_size: %d",
		qp.config.ProcessInterval, qp.config.MaxConcurrentPerEvent, qp.config.BatchSize)

	qp.isRunning = true
	qp.startedAt = time.Now()
	qp.ticker = time.NewTicker(qp.config.ProcessInterval)

	// Start the main processing goroutine
	qp.wg.Add(1)
	go qp.processLoop(ctx)

	qp.logger.Infof(ctx, "Queue processor started successfully")
	return nil
}

func (qp *queueProcessor) Stop() error {
	qp.mu.Lock()
	defer qp.mu.Unlock()

	if !qp.isRunning {
		return errors.New("queue processor is not running")
	}

	qp.logger.Infof(context.Background(), "Stopping queue processor...")

	// Signal stop
	close(qp.stopCh)

	// Stop ticker
	if qp.ticker != nil {
		qp.ticker.Stop()
	}

	// Wait for graceful shutdown with timeout
	done := make(chan struct{})
	go func() {
		qp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		qp.logger.Infof(context.Background(), "Queue processor stopped gracefully")
	case <-time.After(qp.config.ShutdownTimeout):
		qp.logger.Warnf(context.Background(), "Queue processor shutdown timeout exceeded")
	}

	qp.isRunning = false
	return nil
}

func (qp *queueProcessor) processLoop(ctx context.Context) {
	defer qp.wg.Done()

	qp.logger.Infof(ctx, "Queue processor loop started")

	for {
		select {
		case <-ctx.Done():
			qp.logger.Infof(ctx, "Queue processor stopped due to context cancellation")
			return
		case <-qp.stopCh:
			qp.logger.Infof(ctx, "Queue processor stopped due to stop signal")
			return
		case <-qp.ticker.C:
			qp.processAllQueues(ctx)
		}
	}
}

func (qp *queueProcessor) processAllQueues(ctx context.Context) {
	startTime := time.Now()
	defer func() {
		qp.mu.Lock()
		qp.lastProcessed = time.Now()
		qp.mu.Unlock()

		duration := time.Since(startTime)
		if duration > qp.config.MaxProcessingDuration {
			qp.logger.Warnf(ctx, "Queue processing took longer than expected - duration: %v, max: %v",
				duration, qp.config.MaxProcessingDuration)
		}
	}()

	activeEvents, err := qp.getActiveEvents(ctx)
	if err != nil {
		qp.incrementErrorCount()
		qp.logger.Errorf(ctx, "Failed to get active events: %v", err)
		return
	}

	if len(activeEvents) == 0 {
		return
	}

	qp.logger.Debugf(ctx, "Processing queues for active events, event_count: %d", len(activeEvents))

	for _, eventID := range activeEvents {
		if err := qp.ProcessEventQueue(ctx, eventID); err != nil {
			qp.incrementErrorCount()
			qp.logger.Errorf(ctx, "Failed to process queue for event: %v", err)
		}
	}
}

func (qp *queueProcessor) ProcessEventQueue(ctx context.Context, eventID string) error {
	processingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	processingCount, err := qp.queueSvc.GetProcessingCount(processingCtx, eventID)
	if err != nil {
		return fmt.Errorf("failed to get processing count: %w", err)
	}

	availableSlots := int64(qp.config.MaxConcurrentPerEvent) - processingCount
	if availableSlots <= 0 {
		qp.logger.Debugf(processingCtx, "No available slots for event, event_id: %s, processing_count: %d, max_concurrent: %d", eventID, processingCount, qp.config.MaxConcurrentPerEvent)
		return nil
	}

	batchSize := min(availableSlots, int64(qp.config.BatchSize))

	ssIDs, err := qp.queueSvc.PopFromQueue(processingCtx, eventID, int(batchSize))
	if err != nil {
		return fmt.Errorf("failed to pop from queue: %w", err)
	}

	if len(ssIDs) == 0 {
		qp.logger.Debugf(processingCtx, "No sessions to process in queue, event_id: %s", eventID)
		return nil
	}

	qp.logger.Infof(processingCtx, "Starting batch admission, event_id: %s, session_count: %d", eventID, len(ssIDs))

	admittedCount := 0
	admittedSsIDs := make([]string, 0, len(ssIDs))

	for _, sessionID := range ssIDs {
		if err := qp.admitUserToCheckout(processingCtx, eventID, sessionID); err != nil {
			qp.logger.Errorf(processingCtx, "Failed to admit user to checkout, event_id: %s, session_id: %s", eventID, sessionID)
		} else {
			admittedCount++
			admittedSsIDs = append(admittedSsIDs, sessionID)
		}
	}

	qp.mu.Lock()
	qp.totalAdmitted += int64(admittedCount)
	qp.mu.Unlock()

	if len(admittedSsIDs) > 0 {
		if err := qp.queueSvc.PublishPositionUpdate(processingCtx, &models.PositionUpdateEvent{
			EventID:            eventID,
			UpdateType:         models.UpdateTypeUserAdmitted,
			AffectedSessionIDs: admittedSsIDs,
			Timestamp:          time.Now(),
		}); err != nil {
			qp.logger.Warnf(processingCtx, "Failed to publish position update after batch admission - event_id: %s, admitted_count: %d, error: %v",
				eventID, len(admittedSsIDs), err)
		}
	}

	qp.logger.Infof(processingCtx, "Batch processing completed - event_id: %s, attempted: %d, admitted: %d",
		eventID, len(ssIDs), admittedCount)

	return nil
}

func (qp *queueProcessor) admitUserToCheckout(ctx context.Context, eventID, sessionID string) error {
	return qp.withRetry(ctx, func() error {
		return qp.doAdmitUserToCheckout(ctx, eventID, sessionID)
	})
}

func (qp *queueProcessor) doAdmitUserToCheckout(ctx context.Context, eventID, sessionID string) error {
	// 1. Get session
	session, err := qp.sessionSvc.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// 2. Validate session can be admitted
	if !session.CanAdmit() {
		return fmt.Errorf("session cannot be admitted: status=%s, expired=%v",
			session.Status, session.IsExpired())
	}

	// 3. Generate checkout token
	checkoutToken, err := qp.sessionSvc.GenerateCheckoutToken(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to generate checkout token: %w", err)
	}

	// 4. Calculate expiration time
	checkoutExpiresAt := time.Now().Add(15 * time.Minute)

	// 5. Update session with checkout token (atomic operation)
	err = qp.sessionSvc.UpdateCheckoutToken(ctx, sessionID, checkoutToken, checkoutExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to update session with checkout token: %w", err)
	}

	// 6. Add to processing set
	err = qp.queueSvc.AddToProcessing(ctx, eventID, sessionID, 15*time.Minute)
	if err != nil {
		// Try to rollback session update (best effort)
		qp.sessionSvc.UpdateSessionStatus(ctx, sessionID, models.SessionStatusQueued)
		return fmt.Errorf("failed to add to processing: %w", err)
	}

	// 7. Publish QUEUE_READY event (for downstream services)
	err = qp.producer.PublishQueueReady(ctx, kafka.QueueReadyEvent{
		SessionID:     sessionID,
		UserID:        session.UserID,
		EventID:       eventID,
		CheckoutToken: checkoutToken,
		AdmittedAt:    time.Now(),
		ExpiresAt:     checkoutExpiresAt,
		Timestamp:     time.Now(),
	})
	if err != nil {
		qp.logger.Errorf(ctx, "Failed to publish QUEUE_READY event - session_id: %s, error: %v",
			sessionID, err)
	}

	// NOTE: Position update broadcast moved to batch level in ProcessEventQueue
	// to avoid race conditions where users see position -1 before status is updated

	qp.logger.Infof(ctx, "User admitted to checkout successfully - session_id: %s, user_id: %s, event_id: %s, expires_at: %v",
		sessionID, session.UserID, eventID, checkoutExpiresAt)

	return nil
}

func (qp *queueProcessor) getActiveEvents(ctx context.Context) ([]string, error) {
	// Get events from Redis that have active queues
	activeEvents, err := qp.queueSvc.GetActiveEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get active events from Redis: %w", err)
	}

	if len(activeEvents) > 0 {
		qp.logger.Debugf(ctx, "Retrieved active events from Redis - count: %d, events: %v",
			len(activeEvents), activeEvents)
	}

	return activeEvents, nil
}

func (qp *queueProcessor) withRetry(ctx context.Context, operation func() error) error {
	var lastErr error

	for attempt := 0; attempt < qp.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(qp.config.RetryDelay * time.Duration(attempt)):
				// Exponential backoff
			}
		}

		if err := operation(); err != nil {
			lastErr = err
			qp.logger.Warnf(ctx, "Operation failed, retrying - attempt: %d/%d, error: %v",
				attempt+1, qp.config.RetryAttempts, err)
			continue
		}

		return nil // Success
	}

	return fmt.Errorf("operation failed after %d attempts: %w", qp.config.RetryAttempts, lastErr)
}

func (qp *queueProcessor) incrementErrorCount() {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	qp.errorCount++
}

func (qp *queueProcessor) GetStatus() ProcessorStatus {
	qp.mu.RLock()
	defer qp.mu.RUnlock()

	return ProcessorStatus{
		IsRunning:     qp.isRunning,
		StartedAt:     qp.startedAt,
		LastProcessed: qp.lastProcessed,
		TotalAdmitted: qp.totalAdmitted,
		ErrorCount:    qp.errorCount,
	}
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
