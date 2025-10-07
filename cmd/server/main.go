package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	grpcSvc "github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/grpc"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/consumer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka/producer"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/infra/redis"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	pkgGrpc "github.com/vogiaan1904/ticketbottle-waitroom/pkg/grpc"
	pkgKafka "github.com/vogiaan1904/ticketbottle-waitroom/pkg/kafka"
	pkgLog "github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	waitroompb "github.com/vogiaan1904/ticketbottle-waitroom/protogen/waitroom"
	"google.golang.org/grpc"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	l := pkgLog.InitializeZapLogger(pkgLog.ZapConfig{
		Level:    cfg.Log.Level,
		Mode:     cfg.Log.Mode,
		Encoding: cfg.Log.Encoding,
	})

	redisCli, err := redis.Connect(ctx, cfg.Redis)
	if err != nil {
		l.Fatalf(ctx, "Failed to connect to Redis: %v", err)
	}
	defer redis.Disconnect(redisCli)

	ssRepo := repo.NewRedisSessionRepository(redisCli, l)
	qRepo := repo.NewRedisQueueRepository(redisCli, l)

	// Initialize Kafka producer
	kafkaSyncProd, err := pkgKafka.NewProducer(pkgKafka.ProducerConfig{
		Brokers:      cfg.Kafka.Brokers,
		RetryMax:     cfg.Kafka.ProducerRetryMax,
		RequiredAcks: cfg.Kafka.ProducerRequiredAcks,
	})
	if err != nil {
		l.Fatalf(ctx, "Failed to initialize Kafka producer: %v", err)
	}
	defer func() {
		if kafkaSyncProd != nil {
			kafkaSyncProd.Close()
		}
	}()

	// Initialize Kafka consumer
	kafkaConsGr, err := pkgKafka.NewConsumer(pkgKafka.ConsumerConfig{
		Brokers: cfg.Kafka.Brokers,
		GroupID: cfg.Kafka.ConsumerGroupID,
	})
	if err != nil {
		l.Fatalf(ctx, "Failed to initialize Kafka consumer: %v", err)
	}
	defer func() {
		if kafkaConsGr != nil {
			kafkaConsGr.Close()
		}
	}()

	// Queue Producer
	prod := producer.NewProducer(kafkaSyncProd, l)

	// Initialize gRpc Service Clients
	_, eventSvcClose, err := pkgGrpc.NewEventClient(cfg.Microservice.Event)
	if err != nil {
		l.Fatalf(ctx, "Failed to initialize gRpc event service client: %v", err)
	}
	defer eventSvcClose()

	// Initialize services
	ssSvc := service.NewSessionService(ssRepo, cfg.JWT, l)
	qSvc := service.NewQueueService(qRepo, l)
	wrSvc := service.NewWaitroomService(qSvc, ssSvc, prod, l)

	// Waitroom Consumer
	cons := consumer.NewConsumer(kafkaConsGr, wrSvc, l)
	cons.Start(ctx)

	// gRPC server
	wrGrpcSvc := grpcSvc.NewWaitroomGrpcService(wrSvc, l)
	lnr, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.GRpcPort))
	if err != nil {
		l.Fatalf(ctx, "gRPC server failed to listen: %v", err)
	}

	gRpcSrv := grpc.NewServer()
	waitroompb.RegisterWaitroomServiceServer(gRpcSrv, wrGrpcSvc)

	go func() {
		l.Infof(ctx, "gRPC server is listening on port: %d", cfg.Server.GRpcPort)
		if err := gRpcSrv.Serve(lnr); err != nil {
			l.Fatalf(ctx, "Failed to serve gRPC: %v", err)
		}
	}()

	// http server
	// ...

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	l.Info(ctx, "Server shutting down...")

	cancel()
	time.Sleep(1 * time.Second)
	gRpcSrv.GracefulStop()

	l.Info(ctx, "Server exited")
}
