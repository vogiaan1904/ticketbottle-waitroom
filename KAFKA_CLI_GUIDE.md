# Kafka CLI Guide - Complete Reference

## What is a Kafka Cluster?

### Simple Answer
**A Kafka cluster = one or more Kafka broker servers working together**

```
Your Development Setup (1 broker):
┌────────────────┐
│ Kafka Broker 1 │  ← This is your "cluster"
│ (Broker ID: 1) │
└────────────────┘

Production Setup (3 brokers):
┌────────────────┐  ┌────────────────┐  ┌────────────────┐
│ Kafka Broker 1 │  │ Kafka Broker 2 │  │ Kafka Broker 3 │
│ (Broker ID: 1) │  │ (Broker ID: 2) │  │ (Broker ID: 3) │
└────────────────┘  └────────────────┘  └────────────────┘
         \                 |                 /
          \----------------+----------------/
                   Kafka Cluster
```

**Your setup: 1 broker = 1 cluster = Perfect for development!**

---

## Why Kafka UI Showed "Offline"

### The Problem
Kafka UI couldn't find your Kafka broker because:
1. Wrong network configuration
2. Using `localhost` inside Docker doesn't work

### The Fix
```yaml
# docker-compose.dev.yml - FIXED VERSION

kafka:
  container_name: waitroom-kafka
  environment:
    # Two listeners:
    # - Port 29092: For containers (Kafka UI, other services)
    # - Port 9092: For your host machine (your Go code)
    KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://waitroom-kafka:29092,PLAINTEXT_HOST://localhost:9092

kafka-ui:
  environment:
    # Connect using container name + internal port
    KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS: waitroom-kafka:29092
```

**Key Insight:**
- **Inside Docker**: Use container names (`waitroom-kafka:29092`)
- **From host (your code)**: Use localhost (`localhost:9092`)

---

## When to Run create-kafka-topics.sh

### Option 1: Automatic (Recommended) ✅

```bash
# This runs the script automatically:
make docker-up

# Behind the scenes:
# 1. docker-compose up -d  (starts containers)
# 2. sleep 10              (waits for Kafka)
# 3. bash scripts/create-kafka-topics.sh  (creates topics)
```

### Option 2: Manual

```bash
# If you need to recreate topics:
bash scripts/create-kafka-topics.sh

# Or:
make kafka-topics-create
```

### When to Run It?

| Scenario | Need to Run? | Why? |
|----------|-------------|------|
| First time setup | ✅ Yes | Topics don't exist |
| After `docker-compose down` | ❌ No | Topics still exist in volume |
| After `docker-compose down -v` | ✅ Yes | Volumes deleted = topics lost |
| Service restart | ❌ No | Topics persist |
| Added new topic to script | ✅ Yes | Create the new topic |
| Every day | ❌ No | Topics are permanent |

---

## CLI Commands Reference

### 1. List All Topics

```bash
# Using Makefile (easiest)
make kafka-topics-list

# Direct command
docker exec waitroom-kafka kafka-topics \
  --list \
  --bootstrap-server localhost:9092

# Expected output:
CHECKOUT_COMPLETED
CHECKOUT_EXPIRED
CHECKOUT_FAILED
QUEUE_JOINED
QUEUE_LEFT
QUEUE_READY
```

### 2. Describe a Topic (Detailed Info)

```bash
# Show partitions, replicas, etc.
docker exec waitroom-kafka kafka-topics \
  --describe \
  --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED

# Output:
Topic: QUEUE_JOINED     PartitionCount: 5    ReplicationFactor: 1
  Partition: 0    Leader: 1    Replicas: 1    Isr: 1
  Partition: 1    Leader: 1    Replicas: 1    Isr: 1
  ...
```

**What this means:**
- **PartitionCount: 5** - Topic divided into 5 partitions (for parallel processing)
- **ReplicationFactor: 1** - Each partition has 1 copy (no backup in dev)
- **Leader: 1** - Broker 1 handles this partition
- **Isr: 1** - In-Sync Replicas (brokers that are up-to-date)

### 3. Create a Topic Manually

```bash
docker exec waitroom-kafka kafka-topics \
  --create \
  --bootstrap-server localhost:9092 \
  --topic MY_NEW_TOPIC \
  --partitions 5 \
  --replication-factor 1
```

### 4. Delete a Topic

```bash
docker exec waitroom-kafka kafka-topics \
  --delete \
  --bootstrap-server localhost:9092 \
  --topic MY_NEW_TOPIC
```

### 5. Send Test Message (Producer)

```bash
# Using Makefile:
make kafka-console-producer TOPIC=QUEUE_JOINED

# Direct command:
docker exec -it waitroom-kafka kafka-console-producer \
  --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED

# Then type JSON and press Enter:
{"session_id":"test-123","user_id":"user-456","event_id":"concert-2024","position":1}

# Press Ctrl+C to exit
```

### 6. Read Messages (Consumer)

```bash
# Using Makefile:
make kafka-console-consumer TOPIC=QUEUE_JOINED

# Direct command (from beginning):
docker exec -it waitroom-kafka kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED \
  --from-beginning

# Only new messages (skip old ones):
docker exec -it waitroom-kafka kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED

# Press Ctrl+C to exit
```

### 7. Check Consumer Groups

```bash
# List all consumer groups
docker exec waitroom-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --list

# Output:
waitroom-service

# Describe consumer group (shows lag)
docker exec waitroom-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --group waitroom-service \
  --describe

# Output shows:
# - Which topics the group consumes
# - Current offset (message position)
# - Log end offset (latest message)
# - Lag (messages behind)
```

### 8. Check Cluster Info

```bash
# Show all brokers in cluster
docker exec waitroom-kafka kafka-broker-api-versions \
  --bootstrap-server localhost:9092

# Output shows:
# localhost:9092 (id: 1 rack: null) -> (...)
#                     ^ Your single broker
```

### 9. Topic Configuration

```bash
# Show topic settings (retention, compression, etc.)
docker exec waitroom-kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name QUEUE_JOINED \
  --describe

# Change retention (how long messages kept)
docker exec waitroom-kafka kafka-configs \
  --bootstrap-server localhost:9092 \
  --entity-type topics \
  --entity-name QUEUE_JOINED \
  --alter \
  --add-config retention.ms=604800000  # 7 days in milliseconds
```

---

## Visual: How to Check Kafka Status

### Method 1: Kafka UI (Best for Beginners) 🎨

```bash
# Open in browser:
open http://localhost:8090

# You'll see:
┌────────────────────────────────────────┐
│  Kafka UI - Dashboard                  │
├────────────────────────────────────────┤
│  Cluster: local          [Online] ✓   │
│                                         │
│  Topics (6):                            │
│  ├─ QUEUE_READY          10 partitions│
│  ├─ QUEUE_JOINED         5 partitions │
│  ├─ QUEUE_LEFT           5 partitions │
│  ├─ CHECKOUT_COMPLETED   10 partitions│
│  ├─ CHECKOUT_FAILED      10 partitions│
│  └─ CHECKOUT_EXPIRED     10 partitions│
│                                         │
│  Consumer Groups (1):                  │
│  └─ waitroom-service     [Active]     │
└────────────────────────────────────────┘
```

### Method 2: CLI Commands

```bash
# 1. Check if Kafka container is running
docker ps | grep kafka
# Should show: waitroom-kafka ... Up ...

# 2. Check if topics exist
make kafka-topics-list
# Should list 6 topics

# 3. Check Kafka logs
docker logs waitroom-kafka --tail 50
# Should NOT show errors

# 4. Test connection from host
nc -zv localhost 9092
# Should show: Connection to localhost port 9092 [tcp/*] succeeded!
```

### Method 3: Send/Receive Test Message

```bash
# Terminal 1: Start consumer (receiver)
make kafka-console-consumer TOPIC=QUEUE_JOINED

# Terminal 2: Send message (producer)
make kafka-console-producer TOPIC=QUEUE_JOINED
# Type: {"test":"hello"}
# Press Enter

# Terminal 1 should display:
{"test":"hello"}

# ✅ If you see the message = Kafka working!
```

---

## Understanding Your Cluster

### What You Have (Development)

```
Single-Broker Cluster:
┌─────────────────────────────────────────┐
│ Kafka Broker (ID: 1)                    │
│ Container: waitroom-kafka               │
│                                          │
│ Ports:                                   │
│ - 9092: External (your Go code)         │
│ - 29092: Internal (Docker services)     │
│                                          │
│ Topics: 6                                │
│ Partitions: 40 total                    │
│ Replication Factor: 1 (no backups)      │
└─────────────────────────────────────────┘
```

### Topic Breakdown

| Topic | Partitions | Why? |
|-------|-----------|------|
| QUEUE_READY | 10 | High throughput (admitting users) |
| QUEUE_JOINED | 5 | Medium traffic |
| QUEUE_LEFT | 5 | Medium traffic |
| CHECKOUT_COMPLETED | 10 | High throughput (purchases) |
| CHECKOUT_FAILED | 10 | High throughput (retries) |
| CHECKOUT_EXPIRED | 10 | High throughput (timeouts) |

**Why 5-10 partitions?**
- Allows parallel processing
- Can scale to 5-10 consumer instances
- More partitions = better throughput

### Cluster Status

```bash
# Check cluster health
docker exec waitroom-kafka kafka-broker-api-versions \
  --bootstrap-server localhost:9092 | head -1

# Output: localhost:9092 (id: 1 rack: null) -> ...
#                           ^ Broker ID 1 is online

# All brokers online = Cluster is healthy ✓
```

---

## Common Issues & Solutions

### Issue 1: "Connection refused"

```bash
# Symptoms:
Error connecting to node localhost:9092 (id: 1 rack: null)

# Solutions:
1. Check if Kafka is running:
   docker ps | grep kafka

2. Check ports are exposed:
   docker port waitroom-kafka
   # Should show: 9092/tcp -> 0.0.0.0:9092

3. Restart Kafka:
   docker-compose -f docker-compose.dev.yml restart kafka
```

### Issue 2: "Topic does not exist"

```bash
# Solution: Create topics
bash scripts/create-kafka-topics.sh

# Or create single topic:
docker exec waitroom-kafka kafka-topics \
  --create --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED --partitions 5 --replication-factor 1
```

### Issue 3: Kafka UI shows "Offline"

```bash
# Check docker-compose.dev.yml has:
kafka-ui:
  environment:
    KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS: waitroom-kafka:29092
    #                                   ^ Must use container name

# Then restart:
docker-compose -f docker-compose.dev.yml restart kafka-ui
```

### Issue 4: Messages not appearing

```bash
# 1. Check producer sent successfully
echo '{"test":"hi"}' | docker exec -i waitroom-kafka \
  kafka-console-producer \
  --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED

# 2. Check consumer can read
docker exec waitroom-kafka kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED \
  --from-beginning

# Should see your message!
```

---

## Quick Reference Card

```bash
# TOPICS
make kafka-topics-list              # List all topics
make kafka-topics-create            # Create topics from script

# TESTING
make kafka-console-producer TOPIC=QUEUE_JOINED   # Send messages
make kafka-console-consumer TOPIC=QUEUE_JOINED   # Read messages

# MONITORING
docker logs waitroom-kafka          # Kafka logs
docker logs kafka-ui                # UI logs
open http://localhost:8090          # Kafka UI

# TROUBLESHOOTING
docker-compose -f docker-compose.dev.yml restart kafka   # Restart Kafka
docker-compose -f docker-compose.dev.yml down -v         # Full reset (deletes data!)
bash scripts/create-kafka-topics.sh                      # Recreate topics
```

---

## Complete Verification Checklist

Run these commands to verify everything is working:

```bash
# ✅ Step 1: Check containers running
docker ps | grep -E "kafka|zookeeper"
# Should see: waitroom-kafka, waitroom-zookeeper, kafka-ui

# ✅ Step 2: Check topics exist
make kafka-topics-list
# Should list 6 topics

# ✅ Step 3: Check Kafka UI
open http://localhost:8090
# Should show "local" cluster as Online

# ✅ Step 4: Test produce/consume
# Terminal 1:
make kafka-console-consumer TOPIC=QUEUE_JOINED

# Terminal 2:
echo '{"test":"success"}' | docker exec -i waitroom-kafka \
  kafka-console-producer \
  --bootstrap-server localhost:9092 \
  --topic QUEUE_JOINED

# Terminal 1 should display: {"test":"success"}

# ✅ Step 5: Check your app can connect
# Update .env:
KAFKA_ENABLED=true
KAFKA_BROKERS=localhost:9092

# Run your service:
make run

# Should see in logs:
# "Kafka producer initialized successfully"
# "Kafka consumer started successfully"
```

---

## Summary

✅ **Kafka Cluster** = Your single broker (perfect for dev)  
✅ **Topics created** = Like database tables (do once)  
✅ **Check CLI** = Use `make kafka-topics-list`  
✅ **Visual check** = Open http://localhost:8090  
✅ **Test it** = Use producer/consumer commands  

**Your cluster is online when:**
- Docker shows kafka container running
- `make kafka-topics-list` shows 6 topics
- Kafka UI shows "local" cluster as Online
- Test message goes producer → consumer

You're all set! 🎉

