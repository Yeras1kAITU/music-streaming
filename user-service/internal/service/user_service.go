package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/music-streaming/user-service/internal/domain"
	"github.com/music-streaming/user-service/pkg/cache"
	"github.com/music-streaming/user-service/pkg/email"
	"github.com/music-streaming/user-service/pkg/events"
)

type UserService struct {
	userRepo    domain.UserRepository
	sessionRepo domain.SessionRepository
	cache       *cache.UserCache
	email       *email.EmailService
	events      *events.EventPublisher
	jwtSecret   []byte
}

func NewUserService(
	userRepo domain.UserRepository,
	sessionRepo domain.SessionRepository,
	cache *cache.UserCache,
	email *email.EmailService,
	events *events.EventPublisher,
	jwtSecret string,
) *UserService {
	return &UserService{
		userRepo:    userRepo,
		sessionRepo: sessionRepo,
		cache:       cache,
		email:       email,
		events:      events,
		jwtSecret:   []byte(jwtSecret),
	}
}

func (s *UserService) Register(ctx context.Context, email, password, username string) (string, error) {
	if _, err := s.userRepo.GetByEmail(ctx, email); err == nil {
		return "", domain.ErrUserExists
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	userID := uuid.New().String()
	user := &domain.User{
		ID:       userID,
		Email:    email,
		Password: string(hashedPassword),
		Username: username,
		Role:     "user",
		Verified: false,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return "", err
	}

	verifyToken := generateRandomToken(32)
	s.cache.SetVerificationToken(ctx, userID, verifyToken, 24*time.Hour)

	go s.email.SendVerificationEmail(email, userID, verifyToken)
	go s.events.PublishUserRegistered(ctx, userID, email)

	return userID, nil
}

func (s *UserService) Login(ctx context.Context, email, password string) (string, string, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return "", "", domain.ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", "", domain.ErrInvalidCredentials
	}

	if !user.Verified {
		return "", "", domain.ErrEmailNotVerified
	}

	accessToken, refreshToken, err := s.generateTokens(user.ID)
	if err != nil {
		return "", "", err
	}

	session := &domain.Session{
		ID:           uuid.New().String(),
		UserID:       user.ID,
		Token:        accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return "", "", err
	}

	s.cache.SetUser(ctx, user.ID, user, 30*time.Minute)

	return accessToken, refreshToken, nil
}

func (s *UserService) GetUser(ctx context.Context, userID string) (*domain.User, error) {
	if user, err := s.cache.GetUser(ctx, userID); err == nil {
		return user, nil
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	s.cache.SetUser(ctx, userID, user, 30*time.Minute)
	return user, nil
}

func (s *UserService) UpdateUser(ctx context.Context, userID, username, email string) (*domain.User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if username != "" {
		user.Username = username
	}
	if email != "" {
		user.Email = email
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}

	s.cache.InvalidateUser(ctx, userID)
	return user, nil
}

func (s *UserService) DeleteUser(ctx context.Context, userID string) error {
	if err := s.userRepo.Delete(ctx, userID); err != nil {
		return err
	}
	s.sessionRepo.DeleteByUserID(ctx, userID)
	s.cache.InvalidateUser(ctx, userID)
	go s.events.PublishUserDeleted(ctx, userID)
	return nil
}

func (s *UserService) ValidateToken(ctx context.Context, token string) (string, bool) {
	if userID, err := s.cache.GetTokenUser(ctx, token); err == nil {
		return userID, true
	}

	session, err := s.sessionRepo.GetByToken(ctx, token)
	if err != nil {
		return "", false
	}

	parsedToken, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})

	if err != nil || !parsedToken.Valid {
		return "", false
	}

	s.cache.SetTokenUser(ctx, token, session.UserID, 5*time.Minute)
	return session.UserID, true
}

func (s *UserService) Logout(ctx context.Context, token string) error {
	return s.sessionRepo.Delete(ctx, token)
}

func (s *UserService) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return domain.ErrInvalidPassword
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	if err := s.userRepo.UpdatePassword(ctx, userID, string(hashedPassword)); err != nil {
		return err
	}

	s.sessionRepo.DeleteByUserID(ctx, userID)
	return nil
}

func (s *UserService) VerifyEmail(ctx context.Context, userID, token string) error {
	storedToken, err := s.cache.GetVerificationToken(ctx, userID)
	if err != nil || storedToken != token {
		return domain.ErrInvalidToken
	}

	if err := s.userRepo.VerifyEmail(ctx, userID); err != nil {
		return err
	}

	s.cache.DeleteVerificationToken(ctx, userID)
	return nil
}

func (s *UserService) ForgotPassword(ctx context.Context, email string) error {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil // Don't reveal if user exists
	}

	resetToken := generateRandomToken(32)
	s.cache.SetResetToken(ctx, user.ID, resetToken, 1*time.Hour)

	go s.email.SendPasswordResetEmail(email, resetToken)
	return nil
}

func (s *UserService) ResetPassword(ctx context.Context, token, newPassword string) error {
	userID, err := s.cache.GetResetTokenUser(ctx, token)
	if err != nil {
		return domain.ErrInvalidToken
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	if err := s.userRepo.UpdatePassword(ctx, userID, string(hashedPassword)); err != nil {
		return err
	}

	s.cache.DeleteResetToken(ctx, token)
	s.sessionRepo.DeleteByUserID(ctx, userID)

	return nil
}

func (s *UserService) GetUserStats(ctx context.Context, userID string) (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	stats["user_id"] = userID
	return stats, nil
}

func (s *UserService) generateTokens(userID string) (string, string, error) {
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"type":    "access",
		"exp":     time.Now().Add(15 * time.Minute).Unix(),
	})

	accessTokenString, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"type":    "refresh",
		"exp":     time.Now().Add(7 * 24 * time.Hour).Unix(),
	})

	refreshTokenString, err := refreshToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

func generateRandomToken(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
