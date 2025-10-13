# TicketBottle Waitroom System Flow & Architecture

## 📋 Current System Overview

Your waitroom service implements a **virtual queue system** for high-demand ticket sales using **Redis for queue management** and **Kafka for event streaming**. However, you're missing a crucial component: the **Queue Processor**.

## 🏗️ System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    WAITROOM SERVICE                          │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │              │    │              │    │              │  │
│  │ gRPC Server  │    │   Kafka      │    │   Kafka      │  │
│  │  (Port 50051)│    │  Producer    │    │  Consumer    │  │
│  │              │    │              │    │              │  │
│  │ - JoinQueue  │    │ Publishes:   │    │ Consumes:    │  │
│  │ - GetStatus  │    │ - JOINED     │    │ - COMPLETED  │  │
│  │ - LeaveQueue │    │ - LEFT       │    │ - FAILED     │  │
│  │              │    │ - READY ❌    │    │ - EXPIRED    │  │
│  │              │    │              │    │              │  │
│  └───────┬──────┘    └──────┬───────┘    └──────┬───────┘  │
│          │                  │                   │           │
│          └──────────────────┼───────────────────┘           │
│                             │                               │
│                    ┌────────▼────────┐                      │
│                    │                 │                      │
│                    │  Services Layer │                      │
│                    │  - Queue        │                      │
│                    │  - Session      │                      │
│                    │  - Waitroom     │                      │
│                    │                 │                      │
│                    └────────┬────────┘                      │
│                             │                               │
│                    ┌────────▼────────┐                      │
│                    │  Redis Storage  │                      │
│                    │  - Sessions     │                      │
│                    │  - Queues       │                      │
│                    │  - Processing   │                      │
│                    └─────────────────┘                      │
│                                                               │
│  ❌ MISSING: Queue Processor (Background Job)                │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

## 🔄 Complete Flow (What SHOULD Happen)

### 1. User Joins Queue ✅ (IMPLEMENTED)

```
User → gRPC/HTTP → WaitroomService.JoinQueue()
  ├─ SessionService.CreateSession() → Redis
  ├─ QueueService.EnqueueSession() → Redis Sorted Set
  ├─ Producer.PublishQueueJoined() → Kafka
  └─ Return position to user
```

### 2. ❌ Queue Processing (MISSING - The Problem!)

```
Background Goroutine (Every 1-5 seconds):
  ├─ Check available checkout slots
  ├─ Get users from front of queue
  ├─ Generate checkout tokens
  ├─ Update sessions to "admitted" status
  ├─ Add to processing set
  └─ Producer.PublishQueueReady() → Kafka
```

### 3. User Gets Checkout Access ✅ (PARTIALLY IMPLEMENTED)

```
User polls GetQueueStatus():
  ├─ If status = "queued" → Show position
  └─ If status = "admitted" → Show checkout token + URL
```

### 4. Checkout Process ✅ (IMPLEMENTED)

```
Checkout Service receives QUEUE_READY event:
  ├─ Reserve tickets for user
  ├─ User completes payment
  └─ Publishes CHECKOUT_COMPLETED/FAILED/EXPIRED
```

### 5. Cleanup & Next User ✅ (IMPLEMENTED)

```
Waitroom consumes completion events:
  ├─ Update session status
  ├─ Remove from processing set
  └─ Free slot for next user (but no processor to admit them!)
```

## 🗄️ Redis Data Structures

Your system uses **3 Redis data structures** per event:

### 1. Sessions (Hash)

```redis
# Key: waitroom:session:{session_id}
# Value: JSON of session object
HGET waitroom:session:abc-123

{
  "id": "abc-123",
  "user_id": "user-456",
  "event_id": "concert-2024",
  "status": "queued",        # queued → admitted → completed
  "position": 42,
  "checkout_token": "",      # Generated when admitted
  "queued_at": "2024-10-13T10:30:00Z",
  "expires_at": "2024-10-13T12:30:00Z"
}
```

### 2. Queue (Sorted Set)

```redis
# Key: waitroom:{event_id}:queue
# Score: timestamp (FIFO)
# Members: session_ids

ZRANGE waitroom:concert-2024:queue 0 -1 WITHSCORES
1) "session-abc-123"  # First in line
2) "1696248000"       # Joined at timestamp
3) "session-def-456"  # Second in line
4) "1696248015"       # Joined 15 seconds later
```

### 3. Processing Set (Set)

```redis
# Key: waitroom:{event_id}:processing
# Members: session_ids of users currently in checkout

SMEMBERS waitroom:concert-2024:processing
1) "session-xyz-789"  # User in checkout
2) "session-uvw-012"  # User in checkout
# Max 100 concurrent users (configurable)
```

## 📨 Kafka Event Flow

### Events YOU Publish (Producer)

- **QUEUE_JOINED** ✅ - When user joins queue
- **QUEUE_LEFT** ✅ - When user leaves queue
- **QUEUE_READY** ❌ - When user admitted to checkout (MISSING!)

### Events YOU Consume (Consumer)

- **CHECKOUT_COMPLETED** ✅ - User completed purchase
- **CHECKOUT_FAILED** ✅ - Payment failed
- **CHECKOUT_EXPIRED** ✅ - 15-minute timeout

## ❌ What's Missing: Queue Processor

You need a **background goroutine** that runs every 1-5 seconds:

```go
// MISSING IMPLEMENTATION
func (s *waitroomService) StartQueueProcessor(ctx context.Context) {
    ticker := time.NewTicker(time.Duration(s.cfg.Queue.ProcessInterval))
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Process all active events
            events := s.getActiveEvents() // Get from event service
            for _, eventID := range events {
                s.processQueueForEvent(ctx, eventID)
            }
        }
    }
}

func (s *waitroomService) processQueueForEvent(ctx context.Context, eventID string) {
    // 1. Check available slots
    processingCount, _ := s.qSvc.GetProcessingCount(ctx, eventID)
    maxConcurrent := 100 // From config
    availableSlots := maxConcurrent - processingCount

    if availableSlots <= 0 {
        return // No slots available
    }

    // 2. Get users from front of queue
    sessionIDs, _ := s.qSvc.PopFromQueue(ctx, eventID, int(availableSlots))

    for _, sessionID := range sessionIDs {
        // 3. Generate checkout token
        session, _ := s.ssSvc.GetSession(ctx, sessionID)
        token, _ := s.ssSvc.GenerateCheckoutToken(ctx, session)

        // 4. Update session
        s.repo.UpdateCheckoutToken(ctx, sessionID, token, time.Now().Add(15*time.Minute))

        // 5. Add to processing
        s.qSvc.AddToProcessing(ctx, eventID, sessionID, 15*time.Minute)

        // 6. Publish ready event
        s.prod.PublishQueueReady(ctx, kafka.QueueReadyEvent{
            SessionID:     sessionID,
            UserID:        session.UserID,
            EventID:       eventID,
            CheckoutToken: token,
            AdmittedAt:    time.Now(),
            ExpiresAt:     time.Now().Add(15 * time.Minute),
        })
    }
}
```

## 🎯 How Redis Queue Works

### 1. **Joining Queue**

```go
// User joins → Added to sorted set with timestamp score
ZADD waitroom:concert-2024:queue 1696248000 session-abc-123

// Get position (0-indexed rank + 1)
ZRANK waitroom:concert-2024:queue session-abc-123  // Returns 0
// Position = 0 + 1 = 1 (first in line)
```

### 2. **Processing Queue**

```go
// Get next N users from front
ZRANGE waitroom:concert-2024:queue 0 4  // Get first 5 users
// Remove them from queue
ZREM waitroom:concert-2024:queue session-abc-123 session-def-456

// Add to processing set
SADD waitroom:concert-2024:processing session-abc-123 session-def-456
```

### 3. **Completing Checkout**

```go
// Remove from processing when done
SREM waitroom:concert-2024:processing session-abc-123
// This frees up a slot for next user
```

## 🚀 How Kafka Events Work

### 1. **Event Publishing**

```go
// When user joins queue
producer.PublishQueueJoined(kafka.QueueJoinedEvent{
    SessionID: "abc-123",
    EventID:   "concert-2024",
    Position:  42,
    JoinedAt:  time.Now(),
})

// Message goes to Kafka topic: QUEUE_JOINED
// Other services (analytics, notifications) consume it
```

### 2. **Event Consuming**

```go
// Checkout service publishes when payment completes
producer.PublishCheckoutCompleted(CheckoutCompletedEvent{
    SessionID: "abc-123",
    UserID:   "user-456",
    EventID:  "concert-2024",
})

// Your consumer receives it
consumer.HandleCheckoutCompleted(event) {
    // Remove from processing → Free slot for next user
    queueService.RemoveFromProcessing(event.EventID, event.SessionID)

    // Update session status
    sessionService.UpdateSessionStatus(event.SessionID, "completed")
}
```

## 🔧 Configuration

Key config values that control queue behavior:

```go
// config/config.go
type QueueConfig struct {
    DefaultMaxConcurrent   int           // Max users in checkout (100)
    DefaultReleaseRate     int           // Users admitted per batch (10)
    ProcessInterval        time.Duration // How often to check queue (1s)
    SessionTTL             time.Duration // Session expiry (2h)
    PositionUpdateInterval time.Duration // Status polling rate (5s)
}
```

## 🎯 Current Status Summary

| Component           | Status         | Description                         |
| ------------------- | -------------- | ----------------------------------- |
| Join Queue          | ✅ Working     | Users can join, get position        |
| Queue Storage       | ✅ Working     | Redis sorted set + sessions         |
| Event Publishing    | ✅ Partial     | JOINED/LEFT work, READY missing     |
| Event Consuming     | ✅ Working     | Handles checkout completion         |
| **Queue Processor** | ❌ **Missing** | **Core issue - no admission logic** |
| Checkout Tokens     | ✅ Implemented | JWT generation exists               |
| Status Polling      | ✅ Working     | Users can check position            |

## 🛠️ Next Steps

1. **Implement Queue Processor** (Critical!)

   - Background goroutine in main.go
   - Periodic queue processing (every 1-5s)
   - Admit users when slots available
   - Publish QUEUE_READY events

2. **Add Missing Service Interface**

   ```go
   type WaitroomService interface {
       // ... existing methods ...
       StartQueueProcessor(ctx context.Context) error  // ADD THIS
       ProcessQueueForEvent(ctx context.Context, eventID string) error // ADD THIS
   }
   ```

3. **Update Main Server**

   ```go
   // cmd/server/main.go
   go func() {
       wrSvc.StartQueueProcessor(ctx) // ADD THIS
   }()
   ```

4. **Test Complete Flow**
   - Join queue → Check processing starts
   - Complete checkout → Check next user admitted
   - Monitor Kafka events

## 🚨 Why System Currently "Doesn't Work"

Users join the queue and see their position, but **nobody ever gets admitted to checkout** because:

1. ✅ Users join queue (position 1, 2, 3...)
2. ❌ **No processor runs to admit them**
3. ❌ **No QUEUE_READY events published**
4. ❌ **Sessions stay in "queued" status forever**
5. ❌ **Checkout service never gets notified**

The queue just grows infinitely with nobody ever getting admitted!

**Fix: Implement the Queue Processor background job.**

## 📚 Testing the System

```bash
# Start services
make docker-up
make run

# Test joining queue
grpcurl -plaintext -d '{"user_id":"user1","event_id":"concert1"}' \
  localhost:50051 waitroom.v1.WaitroomService/JoinQueue

# Check status (should show position)
grpcurl -plaintext -d '{"session_id":"returned-session-id"}' \
  localhost:50051 waitroom.v1.WaitroomService/GetQueueStatus

# After implementing processor, status should change to "admitted"
# with checkout_token populated
```

---

**The missing Queue Processor is the key to making your system work!**
