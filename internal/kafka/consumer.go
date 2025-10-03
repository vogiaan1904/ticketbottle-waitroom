package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type Consumer interface {
	Start(ctx context.Context) error
	Close() error
}

type kafkaConsumer struct {
	consumerGroup sarama.ConsumerGroup
	topics        []string
	handler       MessageHandler
	logger        logger.Logger
	wg            sync.WaitGroup
}

func NewConsumer(
	brokers []string,
	groupID string,
	topics []string,
	handler MessageHandler,
	logger logger.Logger,
) (Consumer, error) {
	config := sarama.NewConfig()
	config.Version = sarama.V2_8_0_0
	config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	config.Consumer.Return.Errors = true

	consumerGroup, err := sarama.NewConsumerGroup(brokers, groupID, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer group: %w", err)
	}

	logger.Info("Kafka consumer initialized",
		"brokers", brokers,
		"group_id", groupID,
		"topics", topics,
	)

	return &kafkaConsumer{
		consumerGroup: consumerGroup,
		topics:        topics,
		handler:       handler,
		logger:        logger,
	}, nil
}

func (c *kafkaConsumer) processMessage(ctx context.Context, message *sarama.ConsumerMessage) error {
	switch message.Topic {
	case TopicCheckoutCompleted:
		var event CheckoutCompletedEvent
		if err := json.Unmarshal(message.Value, &event); err != nil {
			return fmt.Errorf("failed to unmarshal checkout completed event: %w", err)
		}
		return c.handler.HandleCheckoutCompleted(ctx, event)

	case TopicCheckoutFailed:
		var event CheckoutFailedEvent
		if err := json.Unmarshal(message.Value, &event); err != nil {
			return fmt.Errorf("failed to unmarshal checkout failed event: %w", err)
		}
		return c.handler.HandleCheckoutFailed(ctx, event)

	case TopicCheckoutExpired:
		var event CheckoutExpiredEvent
		if err := json.Unmarshal(message.Value, &event); err != nil {
			return fmt.Errorf("failed to unmarshal checkout expired event: %w", err)
		}
		return c.handler.HandleCheckoutExpired(ctx, event)

	default:
		c.logger.Warn("Unknown topic", "topic", message.Topic)
		return nil
	}
}

func (c *kafkaConsumer) Start(ctx context.Context) error {
	c.wg.Go(func() {
		for {
			// Consume should be called inside an infinite loop
			// When a rebalance happens, the consumer session will need to be recreated
			if err := c.consumerGroup.Consume(ctx, c.topics, c); err != nil {
				c.logger.Error("Error from consumer", "error", err)
			}

			// Check if context was cancelled, signaling that the consumer should stop
			if ctx.Err() != nil {
				c.logger.Info("Consumer context cancelled")
				return
			}
		}
	})

	// Handle errors
	c.wg.Go(func() {
		for err := range c.consumerGroup.Errors() {
			c.logger.Error("Consumer group error", "error", err)
		}
	})

	c.logger.Info("Kafka consumer started")
	return nil
}

func (c *kafkaConsumer) Close() error {
	if err := c.consumerGroup.Close(); err != nil {
		return fmt.Errorf("failed to close consumer group: %w", err)
	}
	c.wg.Wait()
	c.logger.Info("Kafka consumer closed")
	return nil
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (c *kafkaConsumer) Setup(sarama.ConsumerGroupSession) error {
	c.logger.Debug("Consumer group session started")
	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
func (c *kafkaConsumer) Cleanup(sarama.ConsumerGroupSession) error {
	c.logger.Debug("Consumer group session ended")
	return nil
}

// ConsumeClaim must start a consumer loop of ConsumerGroupClaim's Messages()
func (c *kafkaConsumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case message := <-claim.Messages():
			if message == nil {
				return nil
			}

			c.logger.Debug("Received kafka message",
				"topic", message.Topic,
				"partition", message.Partition,
				"offset", message.Offset,
			)

			// Process message
			if err := c.processMessage(session.Context(), message); err != nil {
				c.logger.Error("Failed to process message",
					"topic", message.Topic,
					"offset", message.Offset,
					"error", err,
				)
				// Continue processing other messages even if one fails
				// In production, you might want to implement retry logic or DLQ
			}

			// Mark message as consumed
			session.MarkMessage(message, "")

		case <-session.Context().Done():
			return nil
		}
	}
}
