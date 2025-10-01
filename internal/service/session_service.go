package service

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yourusername/waitroom-service/internal/domain"
	"github.com/yourusername/waitroom-service/internal/repository"
	"github.com/yourusername/waitroom-service/pkg/config"
	"github.com/yourusername/waitroom-service/pkg/logger"
)

type sessionService struct {
	repo   repository.SessionRepository
	config config.JWTConfig
	logger logger.Logger
}

func NewSessionService(
	repo repository.SessionRepository,
	config config.JWTConfig,
	logger logger.Logger,
) SessionService {
	return &sessionService{
		repo:   repo,
		config: config,
		logger: logger,
	}
}

func (s *sessionService) CreateSession(ctx context.Context, userID, eventID string, priority int, userAgent, ipAddress string) (*domain.Session, error) {
	now := time.Now()
	sessionID := uuid.New().String()

	session := &domain.Session{
		ID:               sessionID,
		UserID:           userID,
		EventID:          eventID,
		Position:         0, // Will be updated after adding to queue
		QueuedAt:         now,
		EstimatedWaitSec: 0,
		Status:           domain.SessionStatusQueued,
		ExpiresAt:        now.Add(2 * time.Hour),
		Priority:         priority,
		UserAgent:        userAgent,
		IPAddress:        ipAddress,
		LastHeartbeatAt:  now,
		AttemptCount:     1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	s.logger.Info("Session created",
		"session_id", sessionID,
		"user_id", userID,
		"event_id", eventID,
	)

	return session, nil
}

func (s *sessionService) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	session, err := s.repo.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (s *sessionService) GenerateCheckoutToken(ctx context.Context, session *domain.Session) (string, error) {
	expiresAt := time.Now().Add(s.config.Expiry)

	claims := jwt.MapClaims{
		"session_id": session.ID,
		"user_id":    session.UserID,
		"event_id":   session.EventID,
		"exp":        expiresAt.Unix(),
		"iat":        time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.config.Secret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	s.logger.Debug("Generated checkout token",
		"session_id", session.ID,
		"expires_at", expiresAt,
	)

	return tokenString, nil
}

func (s *sessionService) ValidateSession(ctx context.Context, sessionID string) error {
	session, err := s.repo.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	if session.IsExpired() {
		return domain.ErrSessionExpired
	}

	if !session.IsActive() {
		return domain.ErrInvalidSessionStatus
	}

	return nil
}
