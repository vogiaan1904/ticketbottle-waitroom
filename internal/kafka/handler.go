package kafka

import (
	"context"
	"fmt"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/errors"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

// MessageHandler handles incoming Kafka events
type MessageHandler interface {
	HandleCheckoutCompleted(ctx context.Context, event CheckoutCompletedEvent) error
	HandleCheckoutFailed(ctx context.Context, event CheckoutFailedEvent) error
	HandleCheckoutExpired(ctx context.Context, event CheckoutExpiredEvent) error
}

// CheckoutEventHandler implements MessageHandler
type CheckoutEventHandler struct {
	sessionRepo repo.SessionRepository
	queueRepo   repo.QueueRepository
	logger      logger.Logger
}

// NewCheckoutEventHandler creates a new checkout event handler
func NewCheckoutEventHandler(
	sessionRepo repo.SessionRepository,
	queueRepo repo.QueueRepository,
	logger logger.Logger,
) MessageHandler {
	return &CheckoutEventHandler{
		sessionRepo: sessionRepo,
		queueRepo:   queueRepo,
		logger:      logger,
	}
}

// HandleCheckoutCompleted handles successful checkout completion
func (h *CheckoutEventHandler) HandleCheckoutCompleted(ctx context.Context, event CheckoutCompletedEvent) error {
	h.logger.Info("Handling checkout completed event",
		"session_id", event.SessionID,
		"order_id", event.OrderID,
		"user_id", event.UserID,
		"event_id", event.EventID,
	)

	// Get session
	session, err := h.sessionRepo.Get(ctx, event.SessionID)
	if err != nil {
		if err == errors.ErrSessionNotFound {
			// Session might have expired or been cleaned up
			h.logger.Warn("Session not found for completed checkout",
				"session_id", event.SessionID,
			)
			return nil // Don't retry
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Update session status
	session.Status = models.SessionStatusCompleted
	if err := h.sessionRepo.Update(ctx, session); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	// Remove from processing set
	if err := h.queueRepo.RemoveFromProcessing(ctx, event.EventID, event.SessionID); err != nil {
		h.logger.Error("Failed to remove from processing set",
			"session_id", event.SessionID,
			"event_id", event.EventID,
			"error", err,
		)
		// Continue - not critical
	}

	h.logger.Info("Successfully processed checkout completed event",
		"session_id", event.SessionID,
		"order_id", event.OrderID,
	)

	return nil
}

// HandleCheckoutFailed handles checkout failure
func (h *CheckoutEventHandler) HandleCheckoutFailed(ctx context.Context, event CheckoutFailedEvent) error {
	h.logger.Info("Handling checkout failed event",
		"session_id", event.SessionID,
		"user_id", event.UserID,
		"event_id", event.EventID,
		"reason", event.Reason,
	)

	// Get session
	session, err := h.sessionRepo.Get(ctx, event.SessionID)
	if err != nil {
		if err == errors.ErrSessionNotFound {
			h.logger.Warn("Session not found for failed checkout",
				"session_id", event.SessionID,
			)
			return nil // Don't retry
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Update session status
	session.Status = models.SessionStatusFailed
	if err := h.sessionRepo.Update(ctx, session); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	// Remove from processing set to free up slot
	if err := h.queueRepo.RemoveFromProcessing(ctx, event.EventID, event.SessionID); err != nil {
		h.logger.Error("Failed to remove from processing set",
			"session_id", event.SessionID,
			"event_id", event.EventID,
			"error", err,
		)
		// Continue - not critical
	}

	h.logger.Info("Successfully processed checkout failed event",
		"session_id", event.SessionID,
		"reason", event.Reason,
	)

	return nil
}

// HandleCheckoutExpired handles checkout expiration
func (h *CheckoutEventHandler) HandleCheckoutExpired(ctx context.Context, event CheckoutExpiredEvent) error {
	h.logger.Info("Handling checkout expired event",
		"session_id", event.SessionID,
		"user_id", event.UserID,
		"event_id", event.EventID,
	)

	// Get session
	session, err := h.sessionRepo.Get(ctx, event.SessionID)
	if err != nil {
		if err == errors.ErrSessionNotFound {
			h.logger.Warn("Session not found for expired checkout",
				"session_id", event.SessionID,
			)
			return nil // Don't retry
		}
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Update session status
	session.Status = models.SessionStatusExpired
	if err := h.sessionRepo.Update(ctx, session); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	// Remove from processing set to free up slot for next user
	if err := h.queueRepo.RemoveFromProcessing(ctx, event.EventID, event.SessionID); err != nil {
		h.logger.Error("Failed to remove from processing set",
			"session_id", event.SessionID,
			"event_id", event.EventID,
			"error", err,
		)
		// Continue - not critical
	}

	h.logger.Info("Successfully processed checkout expired event",
		"session_id", event.SessionID,
		"tickets", len(event.Tickets),
	)

	// Note: Queue processor will automatically admit next user when it detects free slot

	return nil
}
