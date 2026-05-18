package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	pb "github.com/music-streaming/proto/payment"
)

type Subscription struct {
	ID        string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    string    `gorm:"type:uuid;not null;index"`
	PlanID    string    `gorm:"not null"`
	PlanName  string    `gorm:"not null"`
	Status    string    `gorm:"default:active"`
	Price     float64   `gorm:"not null"`
	Currency  string    `gorm:"default:USD"`
	StartDate time.Time `gorm:"not null"`
	EndDate   time.Time `gorm:"not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

type PaymentTransaction struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID         string    `gorm:"type:uuid;not null;index"`
	SubscriptionID *string   `gorm:"type:uuid"`
	Amount         float64   `gorm:"not null"`
	Currency       string    `gorm:"default:USD"`
	Status         string    `gorm:"default:pending"`
	PaymentMethod  string    `gorm:"not null"`
	Description    string    `gorm:"type:text"`
	ReceiptURL     string    `gorm:"type:text"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

type Coupon struct {
	ID        string  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Code      string  `gorm:"uniqueIndex;not null"`
	Discount  float64 `gorm:"not null"`
	UsedBy    *string `gorm:"type:uuid"`
	UsedAt    *time.Time
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

type PricingPlan struct {
	ID          string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name        string    `gorm:"not null"`
	Price       float64   `gorm:"not null"`
	Currency    string    `gorm:"default:USD"`
	Interval    string    `gorm:"default:month"`
	Quality     int32     `gorm:"default:128"`
	OfflineMode bool      `gorm:"default:false"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

type paymentServiceServer struct {
	pb.UnimplementedPaymentServiceServer
	db    *gorm.DB
	redis *redis.Client
	nc    *nats.Conn
}

func NewPaymentServiceServer(db *gorm.DB, redis *redis.Client, nc *nats.Conn) *paymentServiceServer {
	return &paymentServiceServer{db: db, redis: redis, nc: nc}
}

func (s *paymentServiceServer) CreateSubscription(ctx context.Context, req *pb.CreateSubscriptionRequest) (*pb.Subscription, error) {
	var plan PricingPlan
	if err := s.db.Where("id = ?", req.PlanId).First(&plan).Error; err != nil {
		return nil, status.Error(codes.NotFound, "plan not found")
	}

	var existing Subscription
	if err := s.db.Where("user_id = ? AND status = ?", req.UserId, "active").First(&existing).Error; err == nil {
		return nil, status.Error(codes.AlreadyExists, "user already has an active subscription")
	}

	subscription := &Subscription{
		ID:        uuid.New().String(),
		UserID:    req.UserId,
		PlanID:    plan.ID,
		PlanName:  plan.Name,
		Status:    "active",
		Price:     plan.Price,
		Currency:  plan.Currency,
		StartDate: time.Now(),
		EndDate:   time.Now().AddDate(0, 1, 0),
	}

	if err := s.db.Create(subscription).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to create subscription")
	}

	event := fmt.Sprintf(`{"event":"subscription_created","subscription_id":"%s","user_id":"%s"}`, subscription.ID, req.UserId)
	s.nc.Publish("payment.events", []byte(event))

	return &pb.Subscription{
		Id:        subscription.ID,
		UserId:    subscription.UserID,
		PlanId:    subscription.PlanID,
		PlanName:  subscription.PlanName,
		Status:    subscription.Status,
		Price:     subscription.Price,
		Currency:  subscription.Currency,
		StartDate: subscription.StartDate.Unix(),
		EndDate:   subscription.EndDate.Unix(),
	}, nil
}

func (s *paymentServiceServer) CancelSubscription(ctx context.Context, req *pb.CancelSubscriptionRequest) (*pb.CancelSubscriptionResponse, error) {
	var subscription Subscription
	if err := s.db.Where("id = ? AND user_id = ?", req.SubscriptionId, req.UserId).First(&subscription).Error; err != nil {
		return nil, status.Error(codes.NotFound, "subscription not found")
	}

	subscription.Status = "cancelled"
	s.db.Save(&subscription)

	event := fmt.Sprintf(`{"event":"subscription_cancelled","subscription_id":"%s","user_id":"%s"}`, subscription.ID, req.UserId)
	s.nc.Publish("payment.events", []byte(event))

	return &pb.CancelSubscriptionResponse{Message: "Subscription cancelled successfully"}, nil
}

func (s *paymentServiceServer) GetSubscription(ctx context.Context, req *pb.GetSubscriptionRequest) (*pb.Subscription, error) {
	var subscription Subscription
	if err := s.db.Where("user_id = ? AND status = ?", req.UserId, "active").First(&subscription).Error; err != nil {
		return nil, status.Error(codes.NotFound, "no active subscription found")
	}

	return &pb.Subscription{
		Id:        subscription.ID,
		UserId:    subscription.UserID,
		PlanId:    subscription.PlanID,
		PlanName:  subscription.PlanName,
		Status:    subscription.Status,
		Price:     subscription.Price,
		Currency:  subscription.Currency,
		StartDate: subscription.StartDate.Unix(),
		EndDate:   subscription.EndDate.Unix(),
	}, nil
}

func (s *paymentServiceServer) ProcessPayment(ctx context.Context, req *pb.ProcessPaymentRequest) (*pb.ProcessPaymentResponse, error) {
	transaction := &PaymentTransaction{
		ID:            uuid.New().String(),
		UserID:        req.UserId,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Status:        "completed",
		PaymentMethod: req.PaymentMethodId,
		Description:   req.Description,
		ReceiptURL:    fmt.Sprintf("https://payments.musicstreaming.com/receipts/%s.pdf", uuid.New().String()),
	}

	if err := s.db.Create(transaction).Error; err != nil {
		return nil, status.Error(codes.Internal, "failed to create transaction")
	}

	event := fmt.Sprintf(`{"event":"payment_completed","transaction_id":"%s","user_id":"%s","amount":%.2f}`,
		transaction.ID, req.UserId, req.Amount)
	s.nc.Publish("payment.events", []byte(event))

	return &pb.ProcessPaymentResponse{
		TransactionId: transaction.ID,
		Success:       true,
		Message:       "Payment processed successfully",
		ReceiptUrl:    transaction.ReceiptURL,
	}, nil
}

func (s *paymentServiceServer) GetPaymentHistory(ctx context.Context, req *pb.GetPaymentHistoryRequest) (*pb.PaymentHistoryResponse, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	var transactions []PaymentTransaction
	offset := (req.Page - 1) * req.PageSize
	var total int64

	s.db.Model(&PaymentTransaction{}).Where("user_id = ?", req.UserId).Count(&total)
	s.db.Where("user_id = ?", req.UserId).Offset(int(offset)).Limit(int(req.PageSize)).Order("created_at DESC").Find(&transactions)

	pbTransactions := make([]*pb.PaymentTransaction, len(transactions))
	for i, t := range transactions {
		pbTransactions[i] = &pb.PaymentTransaction{
			Id:          t.ID,
			UserId:      t.UserID,
			Amount:      t.Amount,
			Currency:    t.Currency,
			Status:      t.Status,
			Description: t.Description,
			CreatedAt:   t.CreatedAt.Unix(),
			ReceiptUrl:  t.ReceiptURL,
		}
	}

	return &pb.PaymentHistoryResponse{Transactions: pbTransactions, Total: int32(total)}, nil
}

func (s *paymentServiceServer) ApplyCoupon(ctx context.Context, req *pb.ApplyCouponRequest) (*pb.ApplyCouponResponse, error) {
	var coupon Coupon
	if err := s.db.Where("code = ? AND expires_at > ?", req.CouponCode, time.Now()).First(&coupon).Error; err != nil {
		return nil, status.Error(codes.NotFound, "invalid or expired coupon")
	}

	if coupon.UsedBy != nil {
		return nil, status.Error(codes.AlreadyExists, "coupon already used")
	}

	coupon.UsedBy = &req.UserId
	now := time.Now()
	coupon.UsedAt = &now
	s.db.Save(&coupon)

	return &pb.ApplyCouponResponse{
		Discount: coupon.Discount,
		Message:  fmt.Sprintf("Coupon applied! You saved %.2f", coupon.Discount),
		NewTotal: 0,
	}, nil
}

func (s *paymentServiceServer) GetInvoice(ctx context.Context, req *pb.GetInvoiceRequest) (*pb.Invoice, error) {
	var transaction PaymentTransaction
	if err := s.db.Where("id = ?", req.TransactionId).First(&transaction).Error; err != nil {
		return nil, status.Error(codes.NotFound, "transaction not found")
	}

	return &pb.Invoice{
		InvoiceId: transaction.ID,
		UserId:    transaction.UserID,
		Amount:    transaction.Amount,
		Currency:  transaction.Currency,
		PdfUrl:    transaction.ReceiptURL,
		Status:    transaction.Status,
		IssuedAt:  transaction.CreatedAt.Unix(),
	}, nil
}

func (s *paymentServiceServer) GetPricingPlans(ctx context.Context, req *pb.GetPricingPlansRequest) (*pb.GetPricingPlansResponse, error) {
	var plans []PricingPlan
	s.db.Find(&plans)

	pbPlans := make([]*pb.PricingPlan, len(plans))
	for i, p := range plans {
		pbPlans[i] = &pb.PricingPlan{
			Id:          p.ID,
			Name:        p.Name,
			Price:       p.Price,
			Currency:    p.Currency,
			Interval:    p.Interval,
			Quality:     p.Quality,
			OfflineMode: p.OfflineMode,
		}
	}

	return &pb.GetPricingPlansResponse{Plans: pbPlans}, nil
}

func (s *paymentServiceServer) UpdatePaymentMethod(ctx context.Context, req *pb.UpdatePaymentMethodRequest) (*pb.UpdatePaymentMethodResponse, error) {
	return &pb.UpdatePaymentMethodResponse{
		PaymentMethodId: req.PaymentMethodId,
		Message:         "Payment method updated successfully",
	}, nil
}

func (s *paymentServiceServer) GetPaymentMethod(ctx context.Context, req *pb.GetPaymentMethodRequest) (*pb.PaymentMethod, error) {
	return &pb.PaymentMethod{
		Id:       "pm_default",
		Last4:    "4242",
		CardType: "Visa",
	}, nil
}

func seedDatabase(db *gorm.DB) {
	var count int64
	db.Model(&PricingPlan{}).Count(&count)
	if count > 0 {
		return
	}

	plans := []PricingPlan{
		{Name: "Free", Price: 0, Currency: "USD", Interval: "month", Quality: 128, OfflineMode: false},
		{Name: "Premium", Price: 9.99, Currency: "USD", Interval: "month", Quality: 320, OfflineMode: true},
		{Name: "Family", Price: 14.99, Currency: "USD", Interval: "month", Quality: 320, OfflineMode: true},
	}

	for _, plan := range plans {
		db.Create(&plan)
	}

	coupons := []Coupon{
		{Code: "WELCOME20", Discount: 20, ExpiresAt: time.Now().AddDate(1, 0, 0)},
		{Code: "SAVE10", Discount: 10, ExpiresAt: time.Now().AddDate(0, 6, 0)},
	}

	for _, coupon := range coupons {
		db.Create(&coupon)
	}
}

func main() {
	dbHost := getEnv("DB_HOST", "postgres")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "music_user")
	dbPass := getEnv("DB_PASSWORD", "music_password")
	dbName := getEnv("DB_NAME", "music_db")
	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	natsURL := getEnv("NATS_URL", "nats://nats:4222")

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Println("Metrics server listening on :9090")
		if err := http.ListenAndServe(":9090", nil); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
		dbHost, dbUser, dbPass, dbName, dbPort)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Info)})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	db.AutoMigrate(&Subscription{}, &PaymentTransaction{}, &Coupon{}, &PricingPlan{})
	seedDatabase(db)

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterPaymentServiceServer(grpcServer, NewPaymentServiceServer(db, redisClient, nc))

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("payment-service", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", ":50053")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Println("Payment service running on port 50053")

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down payment service...")
	grpcServer.GracefulStop()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
