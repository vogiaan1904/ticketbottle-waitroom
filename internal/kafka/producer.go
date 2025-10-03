package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type Producer interface {
	PublishQueueReady(ctx context.Context, event QueueReadyEvent) error
	PublishQueueJoined(ctx context.Context, event QueueJoinedEvent) error
	PublishQueueLeft(ctx context.Context, event QueueLeftEvent) error
	Close() error
}

type kafkaProducer struct {
	producer sarama.SyncProducer
	logger   logger.Logger
}

func NewProducer(brokers []string, config *sarama.Config, logger logger.Logger) (Producer, error) {
	if config == nil {
		config = sarama.NewConfig()
		config.Producer.RequiredAcks = sarama.WaitForLocal
		config.Producer.Retry.Max = 3
		config.Producer.Return.Successes = true
		config.Producer.Return.Errors = true
		config.Producer.Compression = sarama.CompressionSnappy
		config.Producer.MaxMessageBytes = 1000000
	}

	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka producer: %w", err)
	}

	logger.Info("Kafka producer initialized", "brokers", brokers)

	return &kafkaProducer{
		producer: producer,
		logger:   logger,
	}, nil
}

func (p *kafkaProducer) PublishQueueReady(ctx context.Context, event QueueReadyEvent) error {
	event.Timestamp = time.Now()
	return p.publishEvent(TopicQueueReady, event.EventID, event)
}

func (p *kafkaProducer) PublishQueueJoined(ctx context.Context, event QueueJoinedEvent) error {
	event.Timestamp = time.Now()
	return p.publishEvent(TopicQueueJoined, event.EventID, event)
}

func (p *kafkaProducer) PublishQueueLeft(ctx context.Context, event QueueLeftEvent) error {
	event.Timestamp = time.Now()
	return p.publishEvent(TopicQueueLeft, event.EventID, event)
}

func (p *kafkaProducer) publishEvent(topic string, key string, event interface{}) error {
	// Serialize event to JSON
	value, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create Kafka message
	message := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key), // Partition by event_id for ordering
		Value: sarama.ByteEncoder(value),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("timestamp"),
				Value: []byte(time.Now().Format(time.RFC3339)),
			},
		},
	}

	// Send message synchronously
	partition, offset, err := p.producer.SendMessage(message)
	if err != nil {
		p.logger.Error("Failed to send kafka message",
			"topic", topic,
			"error", err,
		)
		return fmt.Errorf("failed to send kafka message: %w", err)
	}

	p.logger.Debug("Kafka message sent",
		"topic", topic,
		"partition", partition,
		"offset", offset,
		"key", key,
	)

	return nil
}

func (p *kafkaProducer) Close() error {
	if err := p.producer.Close(); err != nil {
		return fmt.Errorf("failed to close kafka producer: %w", err)
	}
	p.logger.Info("Kafka producer closed")
	return nil
}
