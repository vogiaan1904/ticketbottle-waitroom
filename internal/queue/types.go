package queue

import (
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/redis/go-redis/v9"
)

type QueueManager struct {
	redis       *redis.Client
	kafka       *kafka.Producer
	rateLimiter RateLimiter
	config      QueueConfig
}

type RateLimiter interface {
	Allow() error
	ReportResult(result error)
}

type QueueConfig struct {
	MaxConcurrentUsers int // Max users in checkout simultaneously
	ReleaseRate        int // Users released per second
	BatchSize          int // Release batch size
	ProcessInterval    time.Duration
}
