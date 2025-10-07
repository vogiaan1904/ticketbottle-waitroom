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
	cli *redis.Client
	l   logger.Logger
}

func NewRedisQueueRepository(cli *redis.Client, l logger.Logger) QueueRepository {
	return &redisQueueRepository{
		cli: cli,
		l:   l,
	}
}

func (r *redisQueueRepository) AddToQueue(ctx context.Context, eID string, ss *models.Session) error {
	qKey := r.queueKey(eID)
	score := ss.GetQueueScore()

	if err := r.cli.ZAdd(ctx, qKey, redis.Z{
		Score:  score,
		Member: ss.ID,
	}).Err(); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.AddToQueue: %v", err)
		return err
	}

	r.l.Debugf(ctx, "Added to queue",
		"event_id", eID,
		"session_id", ss.ID,
		"score", score,
	)

	return nil
}

func (r *redisQueueRepository) RemoveFromQueue(ctx context.Context, eID, ssID string) error {
	qKey := r.queueKey(eID)

	removed, err := r.cli.ZRem(ctx, qKey, ssID).Result()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.RemoveFromQueue: %v", err)
		return err
	}

	if removed > 0 {
		r.l.Debugf(ctx, "Removed from queue",
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

	res, err := script.Run(ctx, r.cli, []string{qKey}, count).Result()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.PopFromQueue: %v", err)
		return nil, err
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
		r.l.Debugf(ctx, "Popped from queue",
			"event_id", eID,
			"count", len(ssIDs),
		)
	}

	return ssIDs, nil
}

func (r *redisQueueRepository) GetQueueLength(ctx context.Context, eID string) (int64, error) {
	qKey := r.queueKey(eID)

	count, err := r.cli.ZCard(ctx, qKey).Result()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.GetQueueLength: %v", err)
		return 0, err
	}

	return count, nil
}

func (r *redisQueueRepository) GetQueuePosition(ctx context.Context, eID, ssID string) (int64, error) {
	qKey := r.queueKey(eID)

	rank, err := r.cli.ZRank(ctx, qKey, ssID).Result()
	if err != nil {
		if err == redis.Nil {
			return -1, nil // Not in queue
		}

		r.l.Errorf(ctx, "redisQueueRepository.GetQueuePosition: %v", err)
		return 0, err
	}

	return rank + 1, nil // Convert to 1-indexed position
}

func (r *redisQueueRepository) GetQueueMembers(ctx context.Context, eID string, start, stop int64) ([]string, error) {
	qKey := r.queueKey(eID)

	members, err := r.cli.ZRange(ctx, qKey, start, stop).Result()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.GetQueueMembers: %v", err)
		return nil, err
	}

	return members, nil
}

func (r *redisQueueRepository) AddToProcessing(ctx context.Context, eID, ssID string, ttl time.Duration) error {
	pKey := r.processingKey(eID)

	pipe := r.cli.Pipeline()
	pipe.SAdd(ctx, pKey, ssID)
	pipe.Expire(ctx, pKey, ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.AddToProcessing: %v", err)
		return err
	}

	r.l.Debugf(ctx, "Added to processing",
		"event_id", eID,
		"session_id", ssID,
		"ttl", ttl,
	)

	return nil
}

func (r *redisQueueRepository) RemoveFromProcessing(ctx context.Context, eID, ssID string) error {
	pKey := r.processingKey(eID)

	if err := r.cli.SRem(ctx, pKey, ssID).Err(); err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.RemoveFromProcessing: %v", err)
		return err
	}

	r.l.Debugf(ctx, "Removed from processing",
		"event_id", eID,
		"session_id", ssID,
	)

	return nil
}

func (r *redisQueueRepository) GetProcessingCount(ctx context.Context, eID string) (int64, error) {
	pKey := r.processingKey(eID)

	count, err := r.cli.SCard(ctx, pKey).Result()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.GetProcessingCount: %v", err)
		return 0, err
	}

	return count, nil
}

func (r *redisQueueRepository) IsProcessing(ctx context.Context, eID, ssID string) (bool, error) {
	pKey := r.processingKey(eID)

	exists, err := r.cli.SIsMember(ctx, pKey, ssID).Result()
	if err != nil {
		r.l.Errorf(ctx, "redisQueueRepository.IsProcessing: %v", err)
		return false, err
	}

	return exists, nil
}

func (r *redisQueueRepository) queueKey(eID string) string {
	return fmt.Sprintf("waitroom:%s:queue", eID)
}

func (r *redisQueueRepository) processingKey(eID string) string {
	return fmt.Sprintf("waitroom:%s:processing", eID)
}
