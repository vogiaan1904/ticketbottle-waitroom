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

	qp.logger.Info(ctx, "Starting queue processor",
		"interval", qp.config.ProcessInterval,
		"max_concurrent_per_event", qp.config.MaxConcurrentPerEvent,
		"batch_size", qp.config.BatchSize,
	)

	qp.isRunning = true
	qp.startedAt = time.Now()
	qp.ticker = time.NewTicker(qp.config.ProcessInterval)

	// Start the main processing goroutine
	qp.wg.Add(1)
	go qp.processLoop(ctx)

	qp.logger.Info(ctx, "Queue processor started successfully")
	return nil
}

func (qp *queueProcessor) Stop() error {
	qp.mu.Lock()
	defer qp.mu.Unlock()

	if !qp.isRunning {
		return errors.New("queue processor is not running")
	}

	qp.logger.Info(context.Background(), "Stopping queue processor...")

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
		qp.logger.Info(context.Background(), "Queue processor stopped gracefully")
	case <-time.After(qp.config.ShutdownTimeout):
		qp.logger.Warn(context.Background(), "Queue processor shutdown timeout exceeded")
	}

	qp.isRunning = false
	return nil
}

func (qp *queueProcessor) processLoop(ctx context.Context) {
	defer qp.wg.Done()

	qp.logger.Info(ctx, "Queue processor loop started")

	for {
		select {
		case <-ctx.Done():
			qp.logger.Info(ctx, "Queue processor stopped due to context cancellation")
			return
		case <-qp.stopCh:
			qp.logger.Info(ctx, "Queue processor stopped due to stop signal")
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
			qp.logger.Warn(ctx, "Queue processing took longer than expected",
				"duration", duration,
				"max_duration", qp.config.MaxProcessingDuration,
			)
		}
	}()

	// Get active events
	activeEvents, err := qp.getActiveEvents(ctx)
	if err != nil {
		qp.incrementErrorCount()
		qp.logger.Error(ctx, "Failed to get active events", "error", err)
		return
	}

	if len(activeEvents) == 0 {
		qp.logger.Debug(ctx, "No active events to process")
		return
	}

	qp.logger.Debug(ctx, "Processing queues for active events",
		"event_count", len(activeEvents))

	// Process each event's queue
	for _, eventID := range activeEvents {
		if err := qp.ProcessEventQueue(ctx, eventID); err != nil {
			qp.incrementErrorCount()
			qp.logger.Error(ctx, "Failed to process queue for event",
				"event_id", eventID,
				"error", err,
			)
			// Continue processing other events even if one fails
		}
	}
}

func (qp *queueProcessor) ProcessEventQueue(ctx context.Context, eventID string) error {
	processingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 1. Check current processing count
	processingCount, err := qp.queueSvc.GetProcessingCount(processingCtx, eventID)
	if err != nil {
		return fmt.Errorf("failed to get processing count: %w", err)
	}

	// 2. Calculate available slots
	availableSlots := int64(qp.config.MaxConcurrentPerEvent) - processingCount
	if availableSlots <= 0 {
		qp.logger.Debug(processingCtx, "No available slots for event",
			"event_id", eventID,
			"processing_count", processingCount,
			"max_concurrent", qp.config.MaxConcurrentPerEvent,
		)
		return nil
	}

	// 3. Determine batch size (min of available slots and configured batch size)
	batchSize := min(availableSlots, int64(qp.config.BatchSize))

	qp.logger.Debug(processingCtx, "Processing queue batch",
		"event_id", eventID,
		"available_slots", availableSlots,
		"batch_size", batchSize,
	)

	// 4. Get users from queue
	sessionIDs, err := qp.queueSvc.PopFromQueue(processingCtx, eventID, int(batchSize))
	if err != nil {
		return fmt.Errorf("failed to pop from queue: %w", err)
	}

	if len(sessionIDs) == 0 {
		qp.logger.Debug(processingCtx, "No sessions to process in queue", "event_id", eventID)
		return nil
	}

	qp.logger.Info(processingCtx, "Admitting users to checkout",
		"event_id", eventID,
		"user_count", len(sessionIDs),
	)

	// 5. Process each session
	admittedCount := 0
	for _, sessionID := range sessionIDs {
		if err := qp.admitUserToCheckout(processingCtx, eventID, sessionID); err != nil {
			qp.logger.Error(processingCtx, "Failed to admit user to checkout",
				"event_id", eventID,
				"session_id", sessionID,
				"error", err,
			)
			// Continue with next user
		} else {
			admittedCount++
		}
	}

	qp.mu.Lock()
	qp.totalAdmitted += int64(admittedCount)
	qp.mu.Unlock()

	qp.logger.Info(processingCtx, "Batch processing completed",
		"event_id", eventID,
		"attempted", len(sessionIDs),
		"admitted", admittedCount,
	)

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

	// 7. Publish QUEUE_READY event
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
		qp.logger.Error(ctx, "Failed to publish QUEUE_READY event",
			"session_id", sessionID,
			"error", err,
		)
	}

	// 8. Broadcast position update to notify all sessions in queue
	if err := qp.queueSvc.PublishPositionUpdate(ctx, &models.PositionUpdateEvent{
		EventID:            eventID,
		UpdateType:         models.UpdateTypeUserAdmitted,
		AffectedSessionIDs: []string{sessionID},
		Timestamp:          time.Now(),
	}); err != nil {
		qp.logger.Warn(ctx, "Failed to publish position update after admission",
			"session_id", sessionID,
			"error", err,
		)
		// Don't fail the request if pub/sub fails
	}

	qp.logger.Info(ctx, "User admitted to checkout successfully",
		"session_id", sessionID,
		"user_id", session.UserID,
		"event_id", eventID,
		"checkout_expires_at", checkoutExpiresAt,
	)

	return nil
}

func (qp *queueProcessor) getActiveEvents(ctx context.Context) ([]string, error) {
	// Get events from Redis that have active queues
	activeEvents, err := qp.queueSvc.GetActiveEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get active events from Redis: %w", err)
	}

	if len(activeEvents) > 0 {
		qp.logger.Debug(ctx, "Retrieved active events from Redis",
			"event_count", len(activeEvents),
			"events", activeEvents)
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
			qp.logger.Warn(ctx, "Operation failed, retrying",
				"attempt", attempt+1,
				"max_attempts", qp.config.RetryAttempts,
				"error", err,
			)
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
