package kafka

import (
	"context"
	"time"
)

type KafkaProducer struct {
	producer sarama.AsyncProducer
	config   KafkaConfig
}

// Events published by Waitroom Service
type QueueReadyEvent struct {
	SessionID     string    `json:"session_id"`
	UserID        string    `json:"user_id"`
	EventID       string    `json:"event_id"`
	CheckoutToken string    `json:"checkout_token"`
	AdmittedAt    time.Time `json:"admitted_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type QueueJoinedEvent struct {
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	EventID   string    `json:"event_id"`
	Position  int64     `json:"position"`
	JoinedAt  time.Time `json:"joined_at"`
}

type QueueLeftEvent struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason"` // user_left, timeout, expired
}

func (kp *KafkaProducer) PublishQueueReady(ctx context.Context, event QueueReadyEvent) error {
	message := &sarama.ProducerMessage{
		Topic: "QUEUE_READY",
		Key:   sarama.StringEncoder(event.EventID), // Partition by event_id
		Value: sarama.ByteEncoder(jsonMarshal(event)),
	}

	kp.producer.Input() <- message
	return nil
}
