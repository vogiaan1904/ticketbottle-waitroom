# Kafka Architecture Best Practices

## Architecture Decision: Single Process ✅

Your waitroom service runs **Producer + Consumer + HTTP Server in ONE process**. This is the **recommended approach** for most microservices.

```
┌─────────────────────────────────────────────────────────────────┐
│                    WAITROOM SERVICE (Single Process)             │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐      │
│  │              │    │              │    │              │      │
│  │ HTTP Server  │    │   Kafka      │    │   Kafka      │      │
│  │  (chi/mux)   │    │  Producer    │    │  Consumer    │      │
│  │              │    │              │    │              │      │
│  │  Port 8080   │    │ Publishes:   │    │ Consumes:    │      │
│  │              │    │ - JOINED     │    │ - COMPLETED  │      │
│  │              │    │ - LEFT       │    │ - FAILED     │      │
│  │              │    │ - READY      │    │ - EXPIRED    │      │
│  │              │    │              │    │              │      │
│  └───────┬──────┘    └──────┬───────┘    └──────┬───────┘      │
│          │                  │                   │               │
│          └──────────────────┼───────────────────┘               │
│                             │                                   │
│                    ┌────────▼────────┐                          │
│                    │                 │                          │
│                    │  Services Layer │                          │
│                    │  - Queue        │                          │
│                    │  - Session      │                          │
│                    │  - Waitroom     │                          │
│                    │                 │                          │
│                    └────────┬────────┘                          │
│                             │                                   │
│                    ┌────────▼────────┐                          │
│                    │  Redis Storage  │                          │
│                    │  - Sessions     │                          │
│                    │  - Queues       │                          │
│                    │  - Processing   │                          │
│                    └─────────────────┘                          │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Why Single Process? 🤔

### ✅ Advantages

1. **Operational Simplicity**
   ```bash
   # One command to run everything
   ./waitroom-service
   
   # One process to monitor
   systemctl status waitroom-service
   
   # One log file
   tail -f /var/log/waitroom-service.log
   ```

2. **Resource Efficiency**
   ```
   Single Process:
   - 1 Redis connection pool
   - 1 Kafka producer connection
   - 1 Kafka consumer group member
   - ~50-100 MB RAM
   
   vs. Separate Processes:
   - 3 Redis connection pools
   - 1 Kafka producer + 1 consumer
   - Multiple process overhead
   - ~150-300 MB RAM
   ```

3. **State Consistency**
   ```go
   // Same Redis connection for both producer and consumer
   // No race conditions or coordination issues
   
   Producer: session written to Redis → Kafka event published
   Consumer: Kafka event received → same Redis updated
   ```

4. **Deployment & Scaling**
   ```yaml
   # Kubernetes - scale the whole service
   replicas: 3  # 3 instances = 3 HTTP + 3 Producer + 3 Consumer
   
   # Each instance is identical
   # No need to manage separate deployments
   ```

5. **Development Experience**
   ```bash
   # Start everything locally
   go run cmd/server/main.go
   
   # Debug in single IDE session
   # Breakpoints work across producer/consumer
   ```

---

## When to Split? (Not Your Case)

Split into separate processes **only if**:

### ❌ Don't Split Unless:

1. **Extremely Different Resource Needs**
   ```
   Consumer: CPU-intensive processing (video encoding)
   Producer: Memory-intensive (caching)
   → Different instance types needed
   ```

2. **Independent Scaling Requirements**
   ```
   Consumer: Needs 10 instances to keep up with messages
   Producer: Only needs 2 instances
   → But Kafka consumer groups already handle this!
   ```

3. **Different Deployment Cycles**
   ```
   Consumer: Updated weekly (stable)
   Producer: Updated daily (feature changes)
   → Rare scenario
   ```

4. **Organizational Boundaries**
   ```
   Consumer: Owned by Team A
   Producer: Owned by Team B
   → Different repos, different deploys
   ```

**Your waitroom service doesn't fit any of these!** ✅

---

## Your Implementation Analysis

### Current Structure (Perfect!)

```go
// cmd/server/main.go
func main() {
    // 1. Load config
    cfg := config.Load()
    
    // 2. Initialize dependencies
    redis := redis.NewClient(cfg.Redis)
    logger := logger.New(cfg.Log)
    
    // 3. Initialize Kafka (conditional)
    if cfg.Kafka.Enabled {
        producer := kafka.NewProducer(...)
        consumer := kafka.NewConsumer(...)
        consumer.Start(ctx)  // Background goroutine
    }
    
    // 4. Initialize services (use producer)
    queueService := service.NewQueueService(..., producer, ...)
    
    // 5. Start HTTP server
    http.ListenAndServe(...)
    
    // 6. Graceful shutdown
    <-quit
    consumer.Close()
    producer.Close()
    server.Shutdown()
}
```

### Lifecycle Management

```
Startup:
1. Config loaded
2. Redis connected
3. Kafka producer initialized
4. Kafka consumer started (background goroutines)
5. HTTP server started (blocking in goroutine)

Running:
- HTTP requests → Queue Service → Kafka Producer
- Kafka Consumer → Event Handler → Redis updates
- All share same Redis connection pool

Shutdown (on SIGTERM):
1. Stop accepting HTTP requests
2. Drain in-flight HTTP requests (30s timeout)
3. Stop Kafka consumer (finish current message)
4. Close Kafka producer (flush pending)
5. Close Redis connection
6. Exit
```

---

## Scaling Strategy

### Horizontal Scaling (Recommended)

```yaml
# docker-compose.yml (local)
services:
  waitroom-service:
    image: waitroom-service:latest
    replicas: 3  # Run 3 identical instances
    ports:
      - "8080-8082:8080"  # Load balance across them

# Kubernetes (production)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: waitroom-service
spec:
  replicas: 5  # 5 pods
  template:
    spec:
      containers:
      - name: waitroom
        image: waitroom-service:latest
        resources:
          requests:
            memory: "128Mi"
            cpu: "250m"
          limits:
            memory: "256Mi"
            cpu: "500m"
```

### How Kafka Consumer Groups Handle Scaling

```
Scenario: 5 replicas + 10 partitions

Instance 1: Partitions 0, 5
Instance 2: Partitions 1, 6
Instance 3: Partitions 2, 7
Instance 4: Partitions 3, 8
Instance 5: Partitions 4, 9

Each message processed by exactly ONE instance
Automatic rebalancing on instance failure
```

---

## Configuration Best Practices

### Environment-Specific Settings

```bash
# Development (.env.local)
KAFKA_ENABLED=true
KAFKA_BROKERS=localhost:9092
KAFKA_CONSUMER_SESSION_TIMEOUT=10000

# Staging (.env.staging)
KAFKA_ENABLED=true
KAFKA_BROKERS=kafka-1:9092,kafka-2:9092
KAFKA_CONSUMER_SESSION_TIMEOUT=30000

# Production (.env.prod)
KAFKA_ENABLED=true
KAFKA_BROKERS=kafka-1.prod:9092,kafka-2.prod:9092,kafka-3.prod:9092
KAFKA_CONSUMER_SESSION_TIMEOUT=60000
KAFKA_PRODUCER_RETRY_MAX=5
KAFKA_PRODUCER_REQUIRED_ACKS=-1  # Wait for all replicas
```

### Feature Flags

```go
// Can disable Kafka without code changes
if cfg.Kafka.Enabled {
    // Full event-driven flow
} else {
    // Degraded mode - still functional
    // Useful for testing, emergencies
}
```

---

## Monitoring Single Process

### Metrics to Track

```go
// HTTP metrics
http_requests_total{method="POST", path="/join", status="200"}
http_request_duration_seconds{path="/join", quantile="0.95"}

// Kafka Producer metrics
kafka_producer_messages_sent_total{topic="QUEUE_JOINED"}
kafka_producer_send_errors_total{topic="QUEUE_JOINED"}
kafka_producer_batch_size_avg

// Kafka Consumer metrics
kafka_consumer_messages_consumed_total{topic="CHECKOUT_COMPLETED"}
kafka_consumer_lag{topic="CHECKOUT_COMPLETED", partition="0"}
kafka_consumer_processing_duration_seconds

// Application metrics
queue_length{event_id="concert-2024"}
sessions_active_total
sessions_completed_total
```

### Health Checks

```go
// GET /health
{
  "status": "healthy",
  "service": "waitroom-service",
  "version": "1.0.0",
  "components": {
    "redis": "healthy",
    "kafka_producer": "healthy",
    "kafka_consumer": "healthy",
    "kafka_consumer_lag": 5  // messages behind
  }
}
```

---

## Alternative Patterns (Reference Only)

### Pattern 1: API + Worker Split (Not Recommended for You)

```
api-service:     HTTP endpoints only
worker-service:  Kafka consumer only

Both share: Same codebase, deployed separately
```

**When useful:**
- API needs to scale independently (50 instances)
- Worker needs high CPU (10 instances with 8 cores)
- Your case: Not needed

### Pattern 2: Microservices Split (Overkill)

```
waitroom-api:       HTTP + Producer only
waitroom-consumer:  Consumer only
waitroom-processor: Background jobs only
```

**When useful:**
- Large teams (10+ engineers)
- Different tech stacks per service
- Your case: Way overkill

### Pattern 3: Serverless (Different Architecture)

```
API Gateway → Lambda (Producer)
Kafka → Lambda (Consumer)
```

**When useful:**
- Extreme variable load
- Pay-per-use model
- Your case: Not needed for ticketing

---

## Summary & Recommendations

### ✅ What You Should Do (Current Setup)

```
Single Process Architecture:
- One main.go
- Producer + Consumer + HTTP Server
- Scale horizontally by running multiple instances
- Kafka consumer group handles partition distribution
```

### ✅ Running in Production

```bash
# Single command
docker run -p 8080:8080 waitroom-service:latest

# Or with docker-compose
docker-compose up -d --scale waitroom-service=3

# Or Kubernetes
kubectl apply -f deployment.yaml
kubectl scale deployment waitroom-service --replicas=5
```

### ✅ Local Development

```bash
# Start dependencies
make docker-up

# Run service (single process)
make run

# Or in multiple terminals for testing
# Terminal 1:
make run

# Terminal 2: Monitor events
make kafka-console-consumer TOPIC=QUEUE_JOINED

# Terminal 3: Test API
curl -X POST http://localhost:8080/api/v1/waitroom/join -d '{...}'
```

---

## Conclusion

**Your current single-process architecture is the RIGHT choice because:**

1. ✅ Simple to operate and deploy
2. ✅ Efficient resource usage
3. ✅ Easy to scale horizontally
4. ✅ Kafka consumer groups handle load distribution
5. ✅ Shared state (Redis) between producer/consumer
6. ✅ Standard industry practice for this scale

**You do NOT need:**
- ❌ Separate cmd/producer/main.go
- ❌ Separate cmd/consumer/main.go
- ❌ Complex orchestration between processes
- ❌ Inter-process communication overhead

**Just run one binary, scale it horizontally, and let Kafka do the heavy lifting!** 🚀

