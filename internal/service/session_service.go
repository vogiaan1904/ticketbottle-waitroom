package service

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
)

type SessionService interface {
	CreateSession(ctx context.Context, uID, eID string, userAgent, ipAddr string) (*models.Session, error)
	UpdateSession(ctx context.Context, ss *models.Session) error
	UpdateSessionStatus(ctx context.Context, sessionID string, status models.SessionStatus) error
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

	s.l.Infof(ctx, "Session created id: %s, user_id: %s, event_id: %s", ssID, uID, eID)

	return ss, nil
}

func (s *sessionService) UpdateSession(ctx context.Context, ss *models.Session) error {
	return s.repo.Update(ctx, ss)
}

func (s *sessionService) UpdateSessionStatus(ctx context.Context, sessionID string, status models.SessionStatus) error {
	if err := s.repo.UpdateStatus(ctx, sessionID, status); err != nil {
		if err == redis.Nil {
			s.l.Warnf(ctx, "sessionService.UpdateSessionStatus: %v", ErrSessionNotFound)
			return ErrSessionNotFound
		}
		s.l.Errorf(ctx, "sessionService.UpdateSessionStatus: %v", err)
		return fmt.Errorf("failed to update session status: %w", err)
	}
	return nil
}

func (s *sessionService) GetSession(ctx context.Context, ssID string) (*models.Session, error) {
	ss, err := s.repo.Get(ctx, ssID)
	if err != nil {
		if err == redis.Nil {
			s.l.Warnf(ctx, "sessionService.GetSession: %v", ErrSessionNotFound)
			return nil, ErrSessionNotFound
		}
		s.l.Errorf(ctx, "sessionService.GetSession: %v", err)
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

	s.l.Debugf(ctx, "Generated checkout token",
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
		return ErrSessionExpired
	}

	if !ss.IsActive() {
		return ErrInvalidSessionStatus
	}

	return nil
}
