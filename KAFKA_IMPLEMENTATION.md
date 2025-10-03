# Kafka Implementation Documentation

## Overview

The Waitroom Service uses Kafka for event-driven communication between microservices. This implementation provides:

- **Event Publishing**: Notify other services when queue state changes
- **Event Consumption**: React to checkout service events
- **Decoupling**: Services communicate asynchronously
- **Scalability**: Handle high throughput with Kafka partitioning
- **Reliability**: Guaranteed delivery with consumer groups

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    WAITROOM SERVICE                          │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────────┐                 ┌──────────────────┐ │
│  │ Kafka Producer   │────────────────▶│  Kafka Topics    │ │
│  │                  │                 │  - QUEUE_READY   │ │
│  │ Publishes:       │                 │  - QUEUE_JOINED  │ │
│  │ - Queue Joined   │                 │  - QUEUE_LEFT    │ │
│  │ - Queue Left     │                 └──────────────────┘ │
│  │ - Queue Ready    │                                       │
│  └──────────────────┘                                       │
│                                                               │
│  ┌──────────────────┐                 ┌──────────────────┐ │
│  │ Kafka Consumer   │◀────────────────│  Kafka Topics    │ │
│  │                  │                 │  - CHECKOUT_*    │ │
│  │ Consumes:        │                 │    COMPLETED     │ │
│  │ - Checkout Done  │                 │    FAILED        │ │
│  │ - Checkout Failed│                 │    EXPIRED       │ │
│  │ - Checkout Expire│                 └──────────────────┘ │
│  └──────────────────┘                                       │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

---

## Event Schemas

### 1. QUEUE_JOINED Event

**Published by:** Waitroom Service  
**Consumed by:** Analytics Service, Notification Service  
**Topic:** `QUEUE_JOINED`  
**Partitioning:** By `event_id`

```json
{
  "session_id": "uuid",
  "user_id": "string",
  "event_id": "string",
  "position": 42,
  "joined_at": "2024-10-02T10:30:00Z",
  "timestamp": "2024-10-02T10:30:00.123Z"
}
```

**Purpose:** Notify when a user joins the queue

### 2. QUEUE_LEFT Event

**Published by:** Waitroom Service  
**Consumed by:** Analytics Service  
**Topic:** `QUEUE_LEFT`  
**Partitioning:** By `event_id`

```json
{
  "session_id": "uuid",
  "user_id": "string",
  "event_id": "string",
  "reason": "user_left|timeout|expired",
  "left_at": "2024-10-02T10:31:00Z",
  "timestamp": "2024-10-02T10:31:00.123Z"
}
```

**Purpose:** Track queue abandonment and reasons

### 3. QUEUE_READY Event

**Published by:** Waitroom Service (Queue Processor)  
**Consumed by:** Checkout Service  
**Topic:** `QUEUE_READY`  
**Partitioning:** By `event_id`

```json
{
  "session_id": "uuid",
  "user_id": "string",
  "event_id": "string",
  "checkout_token": "jwt-token",
  "admitted_at": "2024-10-02T10:35:00Z",
  "expires_at": "2024-10-02T10:50:00Z",
  "timestamp": "2024-10-02T10:35:00.123Z"
}
```

**Purpose:** Admit user to checkout with access token

### 4. CHECKOUT_COMPLETED Event

**Published by:** Checkout Service  
**Consumed by:** Waitroom Service, Inventory Service  
**Topic:** `CHECKOUT_COMPLETED`  
**Partitioning:** By `event_id`

```json
{
  "order_id": "string",
  "session_id": "uuid",
  "user_id": "string",
  "event_id": "string",
  "tickets": [
    {
      "id": "string",
      "seat_no": "A1",
      "category": "VIP",
      "price": 150.00
    }
  ],
  "payment_id": "string",
  "amount": 150.00,
  "timestamp": "2024-10-02T10:40:00Z"
}
```

**Purpose:** Confirm successful purchase and free up processing slot

**Waitroom Service Actions:**
1. Mark session as `completed`
2. Remove from processing set
3. Free up slot for next user

### 5. CHECKOUT_FAILED Event

**Published by:** Checkout Service  
**Consumed by:** Waitroom Service  
**Topic:** `CHECKOUT_FAILED`  
**Partitioning:** By `event_id`

```json
{
  "order_id": "string",
  "session_id": "uuid",
  "user_id": "string",
  "event_id": "string",
  "reason": "payment_failed|inventory_unavailable|validation_error",
  "error_message": "Credit card declined",
  "timestamp": "2024-10-02T10:40:00Z"
}
```

**Purpose:** Handle checkout failures

**Waitroom Service Actions:**
1. Mark session as `failed`
2. Remove from processing set
3. Release slot for next user

### 6. CHECKOUT_EXPIRED Event

**Published by:** Checkout Service  
**Consumed by:** Waitroom Service, Inventory Service  
**Topic:** `CHECKOUT_EXPIRED`  
**Partitioning:** By `event_id`

```json
{
  "order_id": "string",
  "session_id": "uuid",
  "user_id": "string",
  "event_id": "string",
  "tickets": [
    {
      "id": "string",
      "seat_no": "A1",
      "category": "VIP",
      "price": 150.00
    }
  ],
  "expired_at": "2024-10-02T10:50:00Z",
  "timestamp": "2024-10-02T10:50:00Z"
}
```

**Purpose:** Handle checkout timeouts (15-minute window expired)

**Waitroom Service Actions:**
1. Mark session as `expired`
2. Remove from processing set
3. Release tickets back to inventory
4. Admit next user from queue

---

## Code Structure

```
internal/kafka/
├── events.go          # Event type definitions
├── producer.go        # Kafka producer implementation
├── consumer.go        # Kafka consumer implementation
└── handler.go         # Event handler (business logic)
```

### Producer Interface

```go
type Producer interface {
    PublishQueueReady(ctx context.Context, event QueueReadyEvent) error
    PublishQueueJoined(ctx context.Context, event QueueJoinedEvent) error
    PublishQueueLeft(ctx context.Context, event QueueLeftEvent) error
    Close() error
}
```

**Usage in Service Layer:**

```go
// internal/service/queue_service.go
func (s *queueService) EnqueueSession(ctx context.Context, session *domain.Session) (int64, error) {
    // ... add to queue logic ...
    
    // Publish event
    if s.kafkaProducer != nil {
        err := s.kafkaProducer.PublishQueueJoined(ctx, kafka.QueueJoinedEvent{
            SessionID: session.ID,
            UserID:    session.UserID,
            EventID:   session.EventID,
            Position:  position,
            JoinedAt:  session.QueuedAt,
        })
        // Handle error (log but don't fail request)
    }
    
    return position, nil
}
```

### Consumer Interface

```go
type MessageHandler interface {
    HandleCheckoutCompleted(ctx context.Context, event CheckoutCompletedEvent) error
    HandleCheckoutFailed(ctx context.Context, event CheckoutFailedEvent) error
    HandleCheckoutExpired(ctx context.Context, event CheckoutExpiredEvent) error
}
```

**Handler Implementation:**

```go
// internal/kafka/handler.go
type CheckoutEventHandler struct {
    sessionRepo repository.SessionRepository
    queueRepo   repository.QueueRepository
    logger      logger.Logger
}

func (h *CheckoutEventHandler) HandleCheckoutCompleted(ctx context.Context, event CheckoutCompletedEvent) error {
    // 1. Get session
    session, err := h.sessionRepo.Get(ctx, event.SessionID)
    
    // 2. Remove from processing set
    h.queueRepo.RemoveFromProcessing(ctx, event.EventID, event.SessionID)
    
    // 3. Update session status
    session.Status = domain.SessionStatusCompleted
    h.sessionRepo.Update(ctx, session)
    
    return nil
}
```

---

## Configuration

### Environment Variables

```bash
# Enable/disable Kafka
KAFKA_ENABLED=true

# Kafka brokers (comma-separated)
KAFKA_BROKERS=localhost:9092,localhost:9093

# Producer configuration
KAFKA_PRODUCER_TOPIC=QUEUE_READY
KAFKA_PRODUCER_RETRY_MAX=3
KAFKA_PRODUCER_REQUIRED_ACKS=1  # 0=none, 1=leader, -1=all

# Consumer configuration
KAFKA_CONSUMER_GROUP_ID=waitroom-service
KAFKA_CONSUMER_TOPICS=CHECKOUT_COMPLETED,CHECKOUT_FAILED,CHECKOUT_EXPIRED
KAFKA_CONSUMER_SESSION_TIMEOUT=10000  # milliseconds
```

### Topic Configuration

| Topic | Partitions | Replication | Retention |
|-------|-----------|-------------|-----------|
| QUEUE_READY | 10 | 1 | 7 days |
| QUEUE_JOINED | 5 | 1 | 30 days |
| QUEUE_LEFT | 5 | 1 | 30 days |
| CHECKOUT_COMPLETED | 10 | 1 | 90 days |
| CHECKOUT_FAILED | 10 | 1 | 30 days |
| CHECKOUT_EXPIRED | 10 | 1 | 30 days |

**Partitioning Strategy:**
- All topics partitioned by `event_id`
- Ensures ordering of events for the same event
- Allows parallel processing across different events

---

## Message Flow Examples

### Flow 1: User Joins Queue

```
1. User sends POST /api/v1/waitroom/join
   ↓
2. WaitroomService.JoinQueue()
   ↓
3. SessionService.CreateSession() → Create session in Redis
   ↓
4. QueueService.EnqueueSession() → Add to Redis sorted set
   ↓
5. KafkaProducer.PublishQueueJoined() → Send to Kafka
   ↓
6. Kafka Topic: QUEUE_JOINED
   ↓
7. Analytics Service consumes → Track metrics
   Notification Service consumes → Send email/push
```

### Flow 2: Successful Checkout

```
1. Checkout Service processes payment
   ↓
2. Payment successful
   ↓
3. Checkout Service publishes CHECKOUT_COMPLETED
   ↓
4. Kafka Topic: CHECKOUT_COMPLETED
   ↓
5. Waitroom Service Consumer receives event
   ↓
6. CheckoutEventHandler.HandleCheckoutCompleted()
   ↓
7. Update session status to "completed"
   ↓
8. Remove from processing set in Redis
   ↓
9. Queue processor detects free slot
   ↓
10. Admit next user from queue
    ↓
11. Publish QUEUE_READY for next user
```

### Flow 3: Checkout Expired

```
1. Checkout Service detects 15-minute timeout
   ↓
2. Release reserved tickets
   ↓
3. Publish CHECKOUT_EXPIRED event
   ↓
4. Kafka Topic: CHECKOUT_EXPIRED
   ↓
5. Waitroom Service receives event
   ↓
6. Update session status to "expired"
   ↓
7. Remove from processing set
   ↓
8. Inventory Service receives event → Add tickets back
   ↓
9. Queue processor admits next user
```

---

## Error Handling

### Producer Errors

**Strategy:** Log and continue (don't fail main operation)

```go
if err := s.kafkaProducer.PublishQueueJoined(ctx, event); err != nil {
    // Log error but don't return it
    s.logger.Error("Failed to publish queue joined event",
        "session_id", session.ID,
        "error", err,
    )
    // Continue with response to user
}
```

**Rationale:**
- Kafka failures shouldn't block user operations
- Events can be reconstructed from Redis state if needed
- Monitoring alerts on Kafka errors

### Consumer Errors

**Strategy:** Log, skip, and continue processing

```go
func (h *CheckoutEventHandler) HandleCheckoutCompleted(ctx context.Context, event CheckoutCompletedEvent) error {
    session, err := h.sessionRepo.Get(ctx, event.SessionID)
    if err != nil {
        if err == domain.ErrSessionNotFound {
            // Session might have expired, log warning and continue
            h.logger.Warn("Session not found", "session_id", event.SessionID)
            return nil // Don't retry
        }
        // Other errors should be retried
        return fmt.Errorf("failed to get session: %w", err)
    }
    // ... continue processing ...
}
```

**Retry Logic:**
- Transient errors: Consumer group will retry
- Permanent errors: Log and skip message
- No DLQ in current implementation (Week 3+)

---

## Monitoring & Observability

### Key Metrics to Track

1. **Producer Metrics**
   - Messages published per topic
   - Publish failures
   - Publish latency (p50, p95, p99)

2. **Consumer Metrics**
   - Messages consumed per topic
   - Consumer lag (messages behind)
   - Processing errors
   - Processing latency

3. **Business Metrics**
   - Queue join rate
   - Checkout completion rate
   - Checkout failure rate
   - Average time in queue

### Logging

**Producer Logs:**
```json
{
  "level": "info",
  "msg": "Kafka message sent",
  "topic": "QUEUE_JOINED",
  "partition": 3,
  "offset": 12345,
  "key": "concert-2024"
}
```

**Consumer Logs:**
```json
{
  "level": "info",
  "msg": "Handling checkout completed event",
  "session_id": "uuid",
  "order_id": "order-123",
  "user_id": "user-456",
  "event_id": "concert-2024"
}
```

### Health Checks

```go
// Check if Kafka producer is healthy
func (p *kafkaProducer) Health() error {
    // Try to send a test message to a health check topic
    // Or check if connection is still alive
}

// Check if consumer is consuming
func (c *kafkaConsumer) Health() error {
    // Check last message timestamp
    // Alert if no messages in X minutes
}
```

---

## Performance Optimization

### Producer Optimization

```go
config := sarama.NewConfig()
config.Producer.Compression = sarama.CompressionSnappy  // Compress messages
config.Producer.Flush.Messages = 100                     // Batch 100 messages
config.Producer.Flush.Frequency = 100 * time.Millisecond // Or every 100ms
config.Producer.RequiredAcks = sarama.WaitForLocal       // Don't wait for all replicas
```

**Trade-offs:**
- Higher throughput vs. lower latency
- Batching reduces network calls but increases delay
- Compression saves bandwidth but uses CPU

### Consumer Optimization

```go
config := sarama.NewConfig()
config.Consumer.Fetch.Min = 1024                    // Minimum bytes to fetch
config.Consumer.Fetch.Default = 1024 * 1024         // 1MB default fetch
config.Consumer.MaxProcessingTime = 1 * time.Second // Max time per message
```

**Scaling:**
- Add more consumer instances (up to partition count)
- Each instance processes different partitions
- Consumer group rebalancing handles failures

---

## Testing

### Unit Tests

```go
// Test producer publishes correctly
func TestPublishQueueJoined(t *testing.T) {
    mockProducer := &MockProducer{}
    service := NewQueueService(queueRepo, sessionRepo, manager, mockProducer, logger)
    
    session, _ := service.EnqueueSession(ctx, session)
    
    assert.Equal(t, 1, mockProducer.PublishCount)
    assert.Equal(t, "QUEUE_JOINED", mockProducer.LastTopic)
}
```

### Integration Tests

```go
// Test consumer handles events correctly
func TestHandleCheckoutCompleted(t *testing.T) {
    // Setup test containers
    kafka := testcontainers.RunKafkaContainer(t)
    redis := testcontainers.RunRedisContainer(t)
    
    // Create session
    session := createTestSession(t, redis)
    
    // Publish event
    publishEvent(t, kafka, CheckoutCompletedEvent{...})
    
    // Wait and verify
    time.Sleep(1 * time.Second)
    updatedSession := getSession(t, redis, session.ID)
    assert.Equal(t, "completed", updatedSession.Status)
}
```

---

## Deployment Considerations

### Development

```bash
# Use docker-compose
docker-compose up -d kafka

# Single broker, no replication
# Auto-create topics enabled
```

### Staging

```yaml
# Multiple brokers for testing failover
kafka-brokers: kafka-1:9092,kafka-2:9092,kafka-3:9092
replication-factor: 2
min-insync-replicas: 1
```

### Production

```yaml
# Production-grade cluster
kafka-brokers: 
  - kafka-1.prod:9092
  - kafka-2.prod:9092
  - kafka-3.prod:9092
  - kafka-4.prod:9092
  - kafka-5.prod:9092

replication-factor: 3
min-insync-replicas: 2
unclean-leader-election: false  # Prevent data loss
log-retention-hours: 168  # 7 days
```

**Monitoring:**
- Kafka Manager / Kafdrop for UI
- Prometheus exporters for metrics
- Grafana dashboards
- Alert on consumer lag > threshold

---

## Future Enhancements (Post Week 3)

1. **Dead Letter Queue (DLQ)**
   - Automatically route failed messages to DLQ topic
   - Manual review and replay

2. **Idempotency**
   - Add message deduplication
   - Use unique message IDs

3. **Schema Registry**
   - Use Avro/Protobuf for schema evolution
   - Backward/forward compatibility

4. **Transactional Messaging**
   - Ensure exactly-once semantics
   - Coordinate Redis + Kafka updates

5. **Event Sourcing**
   - Store all state changes as events
   - Rebuild state from event log

---

## Troubleshooting Guide

See [KAFKA_TESTING.md](./KAFKA_TESTING.md) for detailed testing and debugging procedures.

**Quick Diagnostics:**

```bash
# Check if topics exist
make kafka-topics-list

# Check consumer group lag
docker exec waitroom-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --group waitroom-service \
  --describe

# View recent messages
make kafka-console-consumer TOPIC=QUEUE_JOINED

# Check service logs
docker-compose logs -f waitroom-service | grep kafka
```

---

## Summary

The Kafka integration provides:

✅ **Asynchronous Communication** - Services don't block each other  
✅ **Event-Driven Architecture** - React to state changes in real-time  
✅ **Scalability** - Handle thousands of events per second  
✅ **Reliability** - Guaranteed message delivery with consumer groups  
✅ **Observability** - Full event audit trail  
✅ **Flexibility** - Easy to add new consumers/producers  

This implementation is production-ready and follows Kafka best practices for microservices communication.