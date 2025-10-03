package service

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/errors"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type SessionService interface {
	CreateSession(ctx context.Context, uID, eID string, userAgent, ipAddr string) (*models.Session, error)
	GetSession(ctx context.Context, ssID string) (*models.Session, error)
	GenerateCheckoutToken(ctx context.Context, ss *models.Session) (string, error)
	ValidateSession(ctx context.Context, ssID string) error
}

type sessionService struct {
	repo repo.SessionRepository
	conf config.JWTConfig
	l    logger.Logger
}

func NewSessionService(
	repo repo.SessionRepository,
	conf config.JWTConfig,
	l logger.Logger,
) SessionService {
	return &sessionService{
		repo: repo,
		conf: conf,
		l:    l,
	}
}

func (s *sessionService) CreateSession(ctx context.Context, uID, eID string, userAgent, ipAddr string) (*models.Session, error) {
	now := time.Now()
	ssID := uuid.New().String()

	ss := &models.Session{
		ID:              ssID,
		UserID:          uID,
		EventID:         eID,
		Position:        0,
		QueuedAt:        now,
		Status:          models.SessionStatusQueued,
		ExpiresAt:       now.Add(2 * time.Hour),
		UserAgent:       userAgent,
		IPAddress:       ipAddr,
		LastHeartbeatAt: now,
		AttemptCount:    1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.Create(ctx, ss); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	s.l.Info("Session created",
		"session_id", ssID,
		"user_id", uID,
		"event_id", eID,
	)

	return ss, nil
}

func (s *sessionService) GetSession(ctx context.Context, ssID string) (*models.Session, error) {
	ss, err := s.repo.Get(ctx, ssID)
	if err != nil {
		return nil, err
	}

	return ss, nil
}

func (s *sessionService) GenerateCheckoutToken(ctx context.Context, ss *models.Session) (string, error) {
	expAt := time.Now().Add(s.conf.Expiry)

	claims := jwt.MapClaims{
		"session_id": ss.ID,
		"user_id":    ss.UserID,
		"event_id":   ss.EventID,
		"exp":        expAt.Unix(),
		"iat":        time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(s.conf.Secret))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	s.l.Debug("Generated checkout token",
		"session_id", ss.ID,
		"expires_at", expAt,
	)

	return tokenStr, nil
}

func (s *sessionService) ValidateSession(ctx context.Context, ssID string) error {
	ss, err := s.repo.Get(ctx, ssID)
	if err != nil {
		return err
	}

	if ss.IsExpired() {
		return errors.ErrSessionExpired
	}

	if !ss.IsActive() {
		return errors.ErrInvalidSessionStatus
	}

	return nil
}
