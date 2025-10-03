#!/bin/bash

# Wait for Kafka to be ready
echo "Waiting for Kafka to be ready..."
sleep 10

# Kafka broker address
KAFKA_BROKER="localhost:9092"

# Topics to create
TOPICS=(
  "QUEUE_READY:10:1"
  "QUEUE_JOINED:5:1"
  "QUEUE_LEFT:5:1"
  "CHECKOUT_COMPLETED:10:1"
  "CHECKOUT_FAILED:10:1"
  "CHECKOUT_EXPIRED:10:1"
)

echo "Creating Kafka topics..."

for topic_config in "${TOPICS[@]}"; do
  IFS=':' read -r topic partitions replication <<< "$topic_config"
  
  echo "Creating topic: $topic (partitions: $partitions, replication: $replication)"
  
  docker exec waitroom-kafka kafka-topics \
    --create \
    --if-not-exists \
    --bootstrap-server localhost:9092 \
    --topic "$topic" \
    --partitions "$partitions" \
    --replication-factor "$replication"
done

echo ""
echo "Listing all topics:"
docker exec waitroom-kafka kafka-topics \
  --list \
  --bootstrap-server localhost:9092

echo ""
echo "Topic details:"
for topic_config in "${TOPICS[@]}"; do
  IFS=':' read -r topic _ _ <<< "$topic_config"
  echo ""
  echo "Topic: $topic"
  docker exec waitroom-kafka kafka-topics \
    --describe \
    --bootstrap-server localhost:9092 \
    --topic "$topic"
done

echo ""
echo "Kafka topics created successfully!"