package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type QueueRepository interface {
	AddToQueue(ctx context.Context, eID string, ss *models.Session) error
	RemoveFromQueue(ctx context.Context, eID, ssID string) error
	PopFromQueue(ctx context.Context, eID string, count int) ([]string, error)
	GetQueueLength(ctx context.Context, eID string) (int64, error)
	GetQueuePosition(ctx context.Context, eID, ssID string) (int64, error)
	GetQueueMembers(ctx context.Context, eID string, start, stop int64) ([]string, error)
	AddToProcessing(ctx context.Context, eID, ssID string, ttl time.Duration) error
	RemoveFromProcessing(ctx context.Context, eID, ssID string) error
	GetProcessingCount(ctx context.Context, eID string) (int64, error)
	IsProcessing(ctx context.Context, eID, ssID string) (bool, error)
}

type redisQueueRepository struct {
	client *redis.Client
	logger logger.Logger
}

func NewRedisQueueRepository(client *redis.Client, logger logger.Logger) QueueRepository {
	return &redisQueueRepository{
		client: client,
		logger: logger,
	}
}

func (r *redisQueueRepository) AddToQueue(ctx context.Context, eID string, ss *models.Session) error {
	qKey := r.queueKey(eID)
	score := ss.GetQueueScore()

	if err := r.client.ZAdd(ctx, qKey, redis.Z{
		Score:  score,
		Member: ss.ID,
	}).Err(); err != nil {
		return fmt.Errorf("failed to add to queue: %w", err)
	}

	r.logger.Debug("Added to queue",
		"event_id", eID,
		"session_id", ss.ID,
		"score", score,
	)

	return nil
}

func (r *redisQueueRepository) RemoveFromQueue(ctx context.Context, eID, ssID string) error {
	qKey := r.queueKey(eID)

	removed, err := r.client.ZRem(ctx, qKey, ssID).Result()
	if err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}

	if removed > 0 {
		r.logger.Debug("Removed from queue",
			"event_id", eID,
			"session_id", ssID,
		)
	}

	return nil
}

func (r *redisQueueRepository) PopFromQueue(ctx context.Context, eID string, count int) ([]string, error) {
	qKey := r.queueKey(eID)

	// Use Lua script for atomic pop operation
	script := redis.NewScript(`
		local key = KEYS[1]
		local count = tonumber(ARGV[1])
		
		local members = redis.call('ZRANGE', key, 0, count - 1)
		if #members > 0 then
			redis.call('ZREM', key, unpack(members))
		end
		
		return members
	`)

	res, err := script.Run(ctx, r.client, []string{qKey}, count).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to pop from queue: %w", err)
	}

	ssIDs := make([]string, 0)
	if resSlice, ok := res.([]interface{}); ok {
		for _, v := range resSlice {
			if id, ok := v.(string); ok {
				ssIDs = append(ssIDs, id)
			}
		}
	}

	if len(ssIDs) > 0 {
		r.logger.Debug("Popped from queue",
			"event_id", eID,
			"count", len(ssIDs),
		)
	}

	return ssIDs, nil
}

func (r *redisQueueRepository) GetQueueLength(ctx context.Context, eID string) (int64, error) {
	qKey := r.queueKey(eID)

	count, err := r.client.ZCard(ctx, qKey).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get queue length: %w", err)
	}

	return count, nil
}

func (r *redisQueueRepository) GetQueuePosition(ctx context.Context, eID, ssID string) (int64, error) {
	qKey := r.queueKey(eID)

	rank, err := r.client.ZRank(ctx, qKey, ssID).Result()
	if err != nil {
		if err == redis.Nil {
			return -1, nil // Not in queue
		}
		return 0, fmt.Errorf("failed to get queue position: %w", err)
	}

	return rank + 1, nil // Convert to 1-indexed position
}

func (r *redisQueueRepository) GetQueueMembers(ctx context.Context, eID string, start, stop int64) ([]string, error) {
	qKey := r.queueKey(eID)

	members, err := r.client.ZRange(ctx, qKey, start, stop).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get queue members: %w", err)
	}

	return members, nil
}

func (r *redisQueueRepository) AddToProcessing(ctx context.Context, eID, ssID string, ttl time.Duration) error {
	pKey := r.processingKey(eID)

	pipe := r.client.Pipeline()
	pipe.SAdd(ctx, pKey, ssID)
	pipe.Expire(ctx, pKey, ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to add to processing: %w", err)
	}

	r.logger.Debug("Added to processing",
		"event_id", eID,
		"session_id", ssID,
		"ttl", ttl,
	)

	return nil
}

func (r *redisQueueRepository) RemoveFromProcessing(ctx context.Context, eID, ssID string) error {
	pKey := r.processingKey(eID)

	if err := r.client.SRem(ctx, pKey, ssID).Err(); err != nil {
		return fmt.Errorf("failed to remove from processing: %w", err)
	}

	r.logger.Debug("Removed from processing",
		"event_id", eID,
		"session_id", ssID,
	)

	return nil
}

func (r *redisQueueRepository) GetProcessingCount(ctx context.Context, eID string) (int64, error) {
	pKey := r.processingKey(eID)

	count, err := r.client.SCard(ctx, pKey).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get processing count: %w", err)
	}

	return count, nil
}

func (r *redisQueueRepository) IsProcessing(ctx context.Context, eID, ssID string) (bool, error) {
	pKey := r.processingKey(eID)

	exists, err := r.client.SIsMember(ctx, pKey, ssID).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check processing status: %w", err)
	}

	return exists, nil
}

func (r *redisQueueRepository) queueKey(eID string) string {
	return fmt.Sprintf("waitroom:%s:queue", eID)
}

func (r *redisQueueRepository) processingKey(eID string) string {
	return fmt.Sprintf("waitroom:%s:processing", eID)
}
