package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/handler"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/queue"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Log.Level, cfg.Log.Format)
	defer log.Sync()

	log.Info("Starting Waitroom Service",
		"version", "1.0.0",
		"env", cfg.Env,
		"port", cfg.Server.Port,
		"kafka_enabled", cfg.Kafka.Enabled,
	)

	// Initialize Redis client
	redisCli, err := redis.NewClient(cfg.Redis)
	if err != nil {
		log.Fatal("Failed to connect to Redis", "error", err)
	}
	defer redisCli.Close()

	// Test Redis connection
	ctx := context.Background()
	if err := redisCli.Ping(ctx).Err(); err != nil {
		log.Fatal("Redis ping failed", "error", err)
	}
	log.Info("Connected to Redis successfully")

	var kafkaProducer kafka.Producer
	var kafkaConsumer kafka.Consumer

	if cfg.Kafka.Enabled {
		// Producer setup
		producerConfig := sarama.NewConfig()
		producerConfig.Producer.RequiredAcks = sarama.RequiredAcks(cfg.Kafka.ProducerRequiredAcks)
		producerConfig.Producer.Retry.Max = cfg.Kafka.ProducerRetryMax
		producerConfig.Producer.Return.Successes = true

		kafkaProducer, err = kafka.NewProducer(cfg.Kafka.Brokers, producerConfig, log)
		if err != nil {
			log.Fatal("Failed to create Kafka producer", "error", err)
		}
		defer kafkaProducer.Close()
		log.Info("Kafka producer initialized successfully")

		// Consumer setup will be done after services are initialized
	} else {
		log.Info("Kafka is disabled")
	}

	sessionRepo := repo.NewRedisSessionRepository(redisCli, log)
	queueRepo := repo.NewRedisQueueRepository(redisCli, log)

	queueManager := queue.NewManager(queueRepo, sessionRepo, cfg.Queue, log)

	sessionService := service.NewSessionService(sessionRepo, cfg.JWT, log)
	queueService := service.NewQueueService(queueRepo, sessionRepo, queueManager, kafkaProducer, log)
	waitroomService := service.NewWaitroomService(queueService, sessionService, log)

	if cfg.Kafka.Enabled {
		eventHandler := kafka.NewCheckoutEventHandler(sessionRepo, queueRepo, log)

		kafkaConsumer, err = kafka.NewConsumer(
			cfg.Kafka.Brokers,
			cfg.Kafka.ConsumerGroupID,
			cfg.Kafka.ConsumerTopics,
			eventHandler,
			log,
		)
		if err != nil {
			log.Fatal("Failed to create Kafka consumer", "error", err)
		}
		defer kafkaConsumer.Close()

		// Start consumer in background
		consumerCtx, consumerCancel := context.WithCancel(context.Background())
		defer consumerCancel()

		if err := kafkaConsumer.Start(consumerCtx); err != nil {
			log.Fatal("Failed to start Kafka consumer", "error", err)
		}
		log.Info("Kafka consumer started successfully")
	}

	httpHandler := handler.NewHTTPHandler(waitroomService, log)

	r := setupRouter(httpHandler, log)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		log.Info("Server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed to start", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Server shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown", "error", err)
	}

	log.Info("Server exited")
}

func setupRouter(h *handler.HTTPHandler, log logger.Logger) *chi.Mux {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(logger.HTTPLogger(log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", h.HealthCheck)

	// API routes
	r.Route("/api/v1/waitroom", func(r chi.Router) {
		r.Post("/join", h.JoinQueue)
		r.Get("/status/{sessionId}", h.GetQueueStatus)
		r.Delete("/leave/{sessionId}", h.LeaveQueue)
	})

	return r
}
