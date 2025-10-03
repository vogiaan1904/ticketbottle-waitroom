package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/errors"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type SessionRepository interface {
	Create(ctx context.Context, ss *models.Session) error
	Get(ctx context.Context, ssID string) (*models.Session, error)
	Update(ctx context.Context, ss *models.Session) error
	UpdateStatus(ctx context.Context, ssID string, status models.SessionStatus) error
	UpdatePosition(ctx context.Context, ssID string, pos int64) error
	UpdateCheckoutToken(ctx context.Context, ssID string, token string, expAt time.Time) error
	Delete(ctx context.Context, ssID string) error
	GetByUserAndEvent(ctx context.Context, uID, eID string) (*models.Session, error)
	Exists(ctx context.Context, ssID string) (bool, error)
}

type redisSessionRepository struct {
	client *redis.Client
	logger logger.Logger
}

func NewRedisSessionRepository(client *redis.Client, logger logger.Logger) SessionRepository {
	return &redisSessionRepository{
		client: client,
		logger: logger,
	}
}

func (r *redisSessionRepository) Create(ctx context.Context, ss *models.Session) error {
	key := r.sessionKey(ss.ID)

	data, err := json.Marshal(ss)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Use pipeline for atomic operation
	pipe := r.client.Pipeline()
	pipe.Set(ctx, key, data, 2*time.Hour)

	// Create user->session index
	ueKey := r.userEventKey(ss.UserID, ss.EventID)
	pipe.Set(ctx, ueKey, ss.ID, 2*time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	r.logger.Debug("Session created",
		"session_id", ss.ID,
		"user_id", ss.UserID,
		"event_id", ss.EventID,
	)

	return nil
}

func (r *redisSessionRepository) Get(ctx context.Context, ssID string) (*models.Session, error) {
	key := r.sessionKey(ssID)

	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, errors.ErrSessionNotFound
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	var ss models.Session
	if err := json.Unmarshal(data, &ss); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return &ss, nil
}

func (r *redisSessionRepository) Update(ctx context.Context, ss *models.Session) error {
	key := r.sessionKey(ss.ID)

	ss.UpdatedAt = time.Now()

	data, err := json.Marshal(ss)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Get current TTL to preserve it
	ttl, err := r.client.TTL(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	if ttl <= 0 {
		ttl = 2 * time.Hour
	}

	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	return nil
}

func (r *redisSessionRepository) UpdateStatus(ctx context.Context, ssID string, status models.SessionStatus) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		return err
	}

	ss.Status = status
	ss.UpdatedAt = time.Now()

	return r.Update(ctx, ss)
}

func (r *redisSessionRepository) UpdatePosition(ctx context.Context, ssID string, pos int64) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		return err
	}

	ss.Position = pos
	ss.UpdatedAt = time.Now()

	return r.Update(ctx, ss)
}

func (r *redisSessionRepository) UpdateCheckoutToken(ctx context.Context, ssID string, token string, expAt time.Time) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		return err
	}

	now := time.Now()
	ss.CheckoutToken = token
	ss.CheckoutExpiresAt = &expAt
	ss.AdmittedAt = &now
	ss.Status = models.SessionStatusAdmitted
	ss.UpdatedAt = now

	return r.Update(ctx, ss)
}

func (r *redisSessionRepository) Delete(ctx context.Context, ssID string) error {
	ss, err := r.Get(ctx, ssID)
	if err != nil {
		if err == errors.ErrSessionNotFound {
			return nil // Already deleted
		}
		return err
	}

	pipe := r.client.Pipeline()

	// Delete session
	pipe.Del(ctx, r.sessionKey(ssID))

	// Delete user->session index
	pipe.Del(ctx, r.userEventKey(ss.UserID, ss.EventID))

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	r.logger.Debug("Session deleted", "session_id", ssID)

	return nil
}

func (r *redisSessionRepository) GetByUserAndEvent(ctx context.Context, uID, eID string) (*models.Session, error) {
	ueKey := r.userEventKey(uID, eID)

	ssID, err := r.client.Get(ctx, ueKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, errors.ErrSessionNotFound
		}
		return nil, fmt.Errorf("failed to get session by user and event: %w", err)
	}

	return r.Get(ctx, ssID)
}

func (r *redisSessionRepository) Exists(ctx context.Context, ssID string) (bool, error) {
	key := r.sessionKey(ssID)
	exists, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check session existence: %w", err)
	}
	return exists > 0, nil
}

func (r *redisSessionRepository) sessionKey(ssID string) string {
	return fmt.Sprintf("waitroom:session:%s", ssID)
}

func (r *redisSessionRepository) userEventKey(uID, eID string) string {
	return fmt.Sprintf("waitroom:user_session:%s:%s", uID, eID)
}
