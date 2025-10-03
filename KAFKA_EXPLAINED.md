# Kafka Explained for Beginners

## What is Kafka? (Simple Answer)

**Kafka is a message delivery system between microservices.**

Think of it like a **super-fast, reliable postal service** for software:
- Services send messages (letters) to topics (mailboxes)
- Other services read those messages
- Messages are stored temporarily (usually 7 days)
- Multiple services can read the same message

---

## Kafka vs Database

| Feature | Database (MySQL/Redis) | Kafka |
|---------|----------------------|-------|
| **Purpose** | Store data permanently | Move messages between services |
| **Operation** | Read/Write/Update/Delete | Publish/Subscribe |
| **Data Flow** | Any direction | One direction (producer → consumer) |
| **Query** | SELECT WHERE ... | Read in order from topic |
| **Persistence** | Forever (until deleted) | Temporary (7-30 days default) |
| **Use Case** | Store user profiles | Notify "user registered" event |

**Key Insight:**
- **Database**: "What is the current state?"
- **Kafka**: "What just happened?"

---

## Your Waitroom System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         YOUR SYSTEM                              │
└─────────────────────────────────────────────────────────────────┘

                    USER JOINS QUEUE
                          │
                          ▼
┌─────────────────────────────────────────────┐
│  Waitroom Service (YOUR CODE)               │
│                                              │
│  1. Save to Redis: session data             │
│  2. Add to Redis: queue position            │
│  3. Send Kafka message: "user joined"       │ ◀── PRODUCER
│                                              │
└──────────────┬───────────────────────────────┘
               │
               │ Kafka Message
               ▼
┌─────────────────────────────────────────────┐
│          KAFKA BROKER (Middleware)          │
│                                              │
│  Topic: QUEUE_JOINED                        │
│  ┌────────────────────────────────────┐    │
│  │ Message 1: {session_id, user_id}   │    │
│  │ Message 2: {session_id, user_id}   │    │
│  │ Message 3: {session_id, user_id}   │    │
│  └────────────────────────────────────┘    │
│                                              │
└──────────────┬───────────────────────────────┘
               │
               │ Multiple consumers can read
               ├─────────────┬────────────────┐
               ▼             ▼                ▼
      ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
      │ Analytics    │ │ Notification │ │ Dashboard    │
      │ Service      │ │ Service      │ │ Service      │
      │              │ │              │ │              │
      │ Tracks       │ │ Sends email  │ │ Updates UI   │
      │ metrics      │ │ to user      │ │ live count   │
      └──────────────┘ └──────────────┘ └──────────────┘
           ▲
           └── CONSUMERS (read messages)
```

---

## Step-by-Step: What Happens

### Scenario: User Joins Queue

```
1. User clicks "Join Queue" button in web app

2. HTTP Request arrives at your Waitroom Service
   POST /api/v1/waitroom/join

3. Your Service does THREE things:
   
   a) Save to Redis (Database)
      session = {id: "abc", user_id: "user-123", status: "queued"}
      HSET sessions:abc {session_data}
      
   b) Add to Redis Queue (Database)
      ZADD waitroom:concert-2024:queue abc 1696248000
      
   c) Send Kafka Message (Event Stream) ← NEW WITH KAFKA
      Topic: QUEUE_JOINED
      Message: {
        "session_id": "abc",
        "user_id": "user-123",
        "event_id": "concert-2024",
        "position": 42,
        "joined_at": "2024-10-02T10:30:00Z"
      }

4. Kafka stores this message in the QUEUE_JOINED topic

5. Other services consuming QUEUE_JOINED receive this message:
   - Analytics Service → Increment "queue joins" counter
   - Notification Service → Send email "You're in line!"
   - Dashboard Service → Update live queue count on website
```

---

## How Kafka Works: Core Concepts

### 1. Topics (Mailboxes)

```
Topics are named channels for messages

Your System Uses:
┌──────────────────────────────────────────┐
│ PUBLISHED BY YOU (Producer):             │
├──────────────────────────────────────────┤
│ QUEUE_JOINED    - User joins queue       │
│ QUEUE_LEFT      - User leaves queue      │
│ QUEUE_READY     - User ready for checkout│
└──────────────────────────────────────────┘

┌──────────────────────────────────────────┐
│ CONSUMED BY YOU (Consumer):              │
├──────────────────────────────────────────┤
│ CHECKOUT_COMPLETED - Purchase successful │
│ CHECKOUT_FAILED    - Payment failed      │
│ CHECKOUT_EXPIRED   - Checkout timeout    │
└──────────────────────────────────────────┘
```

### 2. Messages (The Data)

```json
// Example message in QUEUE_JOINED topic
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "user_id": "user-12345",
  "event_id": "concert-2024",
  "position": 42,
  "joined_at": "2024-10-02T10:30:00Z",
  "timestamp": "2024-10-02T10:30:00.123Z"
}
```

Messages are:
- **Immutable**: Once sent, can't be changed
- **Ordered**: Within a partition, messages keep order
- **Temporary**: Deleted after retention period (7 days default)

### 3. Producers (Senders)

```go
// Your code - internal/kafka/producer.go
func (p *kafkaProducer) PublishQueueJoined(ctx context.Context, event QueueJoinedEvent) error {
    // Convert event to JSON
    data, _ := json.Marshal(event)
    
    // Create Kafka message
    message := &sarama.ProducerMessage{
        Topic: "QUEUE_JOINED",        // Which mailbox
        Key:   sarama.StringEncoder(event.EventID),  // For ordering
        Value: sarama.ByteEncoder(data),             // The actual data
    }
    
    // Send to Kafka
    partition, offset, err := p.client.SendMessage(message)
    
    return err
}
```

**What happens:**
1. Your service calls `PublishQueueJoined()`
2. Producer serializes data to JSON
3. Kafka broker receives and stores message
4. Producer receives confirmation (partition + offset)
5. Message now available for consumers

### 4. Consumers (Receivers)

```go
// Your code - internal/kafka/consumer.go
func (c *kafkaConsumer) ConsumeClaim(session, claim) error {
    for {
        select {
        case message := <-claim.Messages():
            // New message received!
            
            // Deserialize JSON
            var event CheckoutCompletedEvent
            json.Unmarshal(message.Value, &event)
            
            // Process it
            c.handler.HandleCheckoutCompleted(ctx, event)
            
            // Mark as processed
            session.MarkMessage(message, "")
        }
    }
}
```

**What happens:**
1. Consumer continuously listens to topics
2. When message arrives, consumer receives it
3. Consumer processes message (calls your handler)
4. Consumer marks message as "done"
5. Consumer moves to next message

### 5. Consumer Groups (Load Balancing)

```
Scenario: You have 3 instances of Waitroom Service running

Topic: CHECKOUT_COMPLETED (10 partitions)

Kafka automatically distributes:
┌────────────────────────────────────────┐
│ Instance 1 reads: Partitions 0,1,2,3  │
│ Instance 2 reads: Partitions 4,5,6    │
│ Instance 3 reads: Partitions 7,8,9    │
└────────────────────────────────────────┘

Each message processed by ONLY ONE instance
If Instance 2 crashes, Kafka rebalances:
┌────────────────────────────────────────┐
│ Instance 1 reads: Partitions 0,1,2,3,4│
│ Instance 3 reads: Partitions 5,6,7,8,9│
└────────────────────────────────────────┘
```

---

## Setup: YES, Like Database Tables!

You're **absolutely RIGHT** - you need to set up topics first (like CREATE TABLE in databases)!

### Initial Setup (One Time)

```bash
# 1. Start Kafka
docker-compose up -d kafka

# 2. Create topics (like CREATE TABLE)
bash scripts/create-kafka-topics.sh
```

### What create-kafka-topics.sh Does

```bash
# It runs this command for each topic:

kafka-topics --create \
  --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED \
  --partitions 5 \
  --replication-factor 1
```

**This is like:**
```sql
CREATE TABLE queue_joined (
    -- Kafka doesn't define columns, just the topic name
    -- Messages can contain any JSON structure
);
```

### Your 6 Topics Created:

```
QUEUE_READY         - 10 partitions, 1 replica
QUEUE_JOINED        - 5 partitions, 1 replica
QUEUE_LEFT          - 5 partitions, 1 replica
CHECKOUT_COMPLETED  - 10 partitions, 1 replica
CHECKOUT_FAILED     - 10 partitions, 1 replica
CHECKOUT_EXPIRED    - 10 partitions, 1 replica
```

**Key Difference from Database:**
- Database: Define strict schema (columns, types)
- Kafka: Just topic name, messages can be any JSON

---

## Complete Flow in Your System

### Flow 1: User Joins Queue (You PRODUCE)

```
1. User Request
   POST /api/v1/waitroom/join
   Body: {"user_id": "user-123", "event_id": "concert-2024"}

2. Your HTTP Handler
   handler.JoinQueue() receives request

3. Waitroom Service Layer
   waitroomService.JoinQueue()
   ├─ Creates session in Redis
   ├─ Adds to queue in Redis
   └─ Returns position to user

4. Queue Service (Producer)
   queueService.EnqueueSession()
   └─ kafkaProducer.PublishQueueJoined()
      └─ Sends message to QUEUE_JOINED topic

5. Kafka stores message

6. Other services consume (Analytics, Notifications, etc.)
```

### Flow 2: Checkout Completes (You CONSUME)

```
1. Checkout Service (separate microservice)
   User completes payment
   └─ Publishes to CHECKOUT_COMPLETED topic

2. Kafka stores message

3. Your Consumer receives message
   kafkaConsumer running in background
   └─ Receives message from CHECKOUT_COMPLETED

4. Your Event Handler
   CheckoutEventHandler.HandleCheckoutCompleted()
   ├─ Reads session from Redis
   ├─ Updates session status to "completed"
   ├─ Removes from processing set
   └─ Frees up slot for next user

5. Queue Processor
   Detects free slot
   └─ Admits next user from queue
```

---

## Redis vs Kafka in Your System

### Why Use Both?

```
Redis (Database):
✅ Store current state
✅ Fast lookups (get session by ID)
✅ Queue management (sorted sets)
✅ Session data persistence
✅ Cache layer

Kafka (Message Bus):
✅ Notify other services
✅ Event history/audit trail
✅ Decouple services
✅ Asynchronous processing
✅ Fan-out to multiple consumers

Both are COMPLEMENTARY, not replacements!
```

### Example:

```
User joins queue:

1. Write to Redis:
   HSET sessions:abc {id: "abc", status: "queued"}
   ZADD queue:concert-2024 1696248000 abc
   → Redis knows current state

2. Send to Kafka:
   QUEUE_JOINED → {session_id: "abc", ...}
   → Kafka notifies other services about the event

If Kafka is down:
- User can still join queue (Redis works)
- Other services won't be notified (degraded, but functional)

If Redis is down:
- Can't join queue (critical dependency)
- Kafka messages can't be processed (need Redis to lookup sessions)
```

---

## Common Questions

### Q: Do I query Kafka like a database?

**No!** Kafka is a stream, not a database.

```
Database:
SELECT * FROM users WHERE age > 18

Kafka:
- You can't query
- You read messages in order
- Messages flow from producer to consumer
```

### Q: Can I delete/update Kafka messages?

**No!** Messages are immutable.

```
Database:
UPDATE sessions SET status = 'completed' WHERE id = 'abc'

Kafka:
- Send new message: {session_id: "abc", status: "completed"}
- Old messages remain (until retention period expires)
- Consumers process new message
```

### Q: What if consumer crashes midway?

**Kafka tracks your position!**

```
Consumer reads:
Message 1 ✓ (marked)
Message 2 ✓ (marked)
Message 3 ✗ (crash!)

Consumer restarts:
Kafka: "You last processed message 2"
Consumer resumes from message 3
```

### Q: How does Kafka store messages?

**On disk (like database), but optimized for sequential writes**

```
/var/lib/kafka/data/
  QUEUE_JOINED-0/  (partition 0)
    00000000000000000000.log  (message file)
  QUEUE_JOINED-1/  (partition 1)
    00000000000000000000.log
```

### Q: When are messages deleted?

**Based on retention policy (time or size)**

```
# Default in your system: 7 days
After 7 days, old messages are deleted automatically

You can configure:
- Time-based: retention.ms=604800000 (7 days)
- Size-based: retention.bytes=1073741824 (1 GB)
```

---

## Testing Your Kafka Setup

### 1. Check Topics Exist

```bash
make kafka-topics-list

# Output:
CHECKOUT_COMPLETED
CHECKOUT_EXPIRED
CHECKOUT_FAILED
QUEUE_JOINED
QUEUE_LEFT
QUEUE_READY
```

### 2. Manual Produce (Send Test Message)

```bash
# Open producer console
make kafka-console-producer TOPIC=QUEUE_JOINED

# Type message and press Enter:
{"session_id": "test-123", "user_id": "user-456", "event_id": "concert-2024", "position": 1, "joined_at": "2024-10-02T10:00:00Z"}
```

### 3. Manual Consume (Read Messages)

```bash
# In another terminal
make kafka-console-consumer TOPIC=QUEUE_JOINED

# You'll see the message you sent!
{"session_id": "test-123", ...}
```

### 4. Test Your Service

```bash
# Start your service
make run

# In another terminal, make API call
curl -X POST http://localhost:8080/api/v1/waitroom/join \
  -H "Content-Type: application/json" \
  -d '{"user_id": "user-001", "event_id": "concert-2024", "priority": 0}'

# Check Kafka received the message
make kafka-console-consumer TOPIC=QUEUE_JOINED

# You should see a message appear!
```

---

## Quick Reference

### Kafka Commands (via Makefile)

```bash
# List all topics
make kafka-topics-list

# Create topics (if not exists)
make kafka-topics-create

# Read messages from topic
make kafka-console-consumer TOPIC=QUEUE_JOINED

# Send message to topic
make kafka-console-producer TOPIC=QUEUE_JOINED

# Check consumer group status
docker exec waitroom-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --group waitroom-service \
  --describe
```

### Key Kafka Concepts Summary

| Concept | Analogy | Purpose |
|---------|---------|---------|
| **Broker** | Post office | Stores and routes messages |
| **Topic** | Mailbox | Named channel for messages |
| **Partition** | Mailbox section | Enables parallel processing |
| **Producer** | Sender | Publishes messages to topics |
| **Consumer** | Receiver | Reads messages from topics |
| **Consumer Group** | Team | Distributes work across instances |
| **Offset** | Page number | Tracks reading position |
| **Key** | Routing code | Determines which partition |

---

## Summary

✅ **Kafka is NOT a database** - it's a message bus  
✅ **Topics are like mailboxes** - named channels for messages  
✅ **You produce events** - notify other services about what happened  
✅ **You consume events** - react to other services' events  
✅ **Setup is like CREATE TABLE** - create topics first  
✅ **Redis stores state** - Kafka notifies about changes  
✅ **Single process runs everything** - producer + consumer + HTTP server  

**Your system now has event-driven communication!** 🎉

When a user joins the queue:
1. State saved to Redis (database)
2. Event sent to Kafka (message bus)
3. Other services notified automatically

This is the foundation of modern microservices architecture!
