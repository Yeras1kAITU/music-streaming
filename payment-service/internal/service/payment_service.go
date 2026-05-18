package service

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type PaymentService struct {
	transactionRepo  domain.PaymentTransactionRepository
	subscriptionRepo domain.SubscriptionRepository
	events           *events.EventPublisher
	redis            *redis.Client
}

func NewPaymentService(
	transactionRepo domain.PaymentTransactionRepository,
	subscriptionRepo domain.SubscriptionRepository,
	events *events.EventPublisher,
	redis *redis.Client,
) *PaymentService {
	return &PaymentService{
		transactionRepo:  transactionRepo,
		subscriptionRepo: subscriptionRepo,
		events:           events,
		redis:            redis,
	}
}

func (s *PaymentService) ProcessPayment(ctx context.Context, userID string, amount float64, currency, paymentMethodID, description string) (*domain.PaymentTransaction, error) {
	// Check rate limit
	rateKey := fmt.Sprintf("rate:payment:%s", userID)
	count, err := s.redis.Incr(ctx, rateKey).Result()
	if err == nil && count > 10 {
		return nil, domain.ErrRateLimitExceeded
	}
	s.redis.Expire(ctx, rateKey, time.Hour)

	// Create transaction
	transaction := &domain.PaymentTransaction{
		ID:            uuid.New().String(),
		UserID:        userID,
		Amount:        amount,
		Currency:      currency,
		Status:        "pending",
		PaymentMethod: paymentMethodID,
		Description:   description,
		CreatedAt:     time.Now(),
	}

	if err := s.transactionRepo.Create(ctx, transaction); err != nil {
		return nil, err
	}

	// Process payment with external provider (simulated)
	success, err := s.callPaymentGateway(transaction)
	if err != nil {
		transaction.Status = "failed"
		s.transactionRepo.Update(ctx, transaction)
		return nil, err
	}

	if success {
		transaction.Status = "completed"
		s.transactionRepo.Update(ctx, transaction)

		// Publish event
		s.events.PublishPaymentCompleted(ctx, transaction.ID, userID, amount)

		// Update subscription if exists
		if sub, err := s.subscriptionRepo.GetByUserID(ctx, userID); err == nil && sub.Status == "active" {
			sub.EndDate = sub.EndDate.AddDate(0, 1, 0)
			s.subscriptionRepo.Update(ctx, sub)
		}
	} else {
		transaction.Status = "failed"
		s.transactionRepo.Update(ctx, transaction)
	}

	return transaction, nil
}

func (s *PaymentService) GetPaymentHistory(ctx context.Context, userID string, page, pageSize int32) ([]domain.PaymentTransaction, int64, error) {
	return s.transactionRepo.GetByUserID(ctx, userID, page, pageSize)
}

func (s *PaymentService) GetTransaction(ctx context.Context, transactionID string) (*domain.PaymentTransaction, error) {
	return s.transactionRepo.GetByID(ctx, transactionID)
}

func (s *PaymentService) callPaymentGateway(transaction *domain.PaymentTransaction) (bool, error) {
	// Simulate payment gateway call
	// In production, integrate with Stripe, PayPal, etc.
	time.Sleep(500 * time.Millisecond)

	// Simulate 95% success rate
	if time.Now().UnixNano()%100 < 95 {
		return true, nil
	}
	return false, fmt.Errorf("payment declined")
}
