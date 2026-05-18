package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "os"
    "time"

    "github.com/go-redis/redis/v8"
    "github.com/google/uuid"
    "github.com/nats-io/nats.go"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/health"
    "google.golang.org/grpc/health/grpc_health_v1"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

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
    ID            string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    UserID        string    `gorm:"type:uuid;not null;index"`
    SubscriptionID *string   `gorm:"type:uuid"`
    Amount        float64   `gorm:"not null"`
    Currency      string    `gorm:"default:USD"`
    Status        string    `gorm:"default:pending"`
    PaymentMethod string    `gorm:"not null"`
    Description   string    `gorm:"type:text"`
    ReceiptURL    string    `gorm:"type:text"`
    CreatedAt     time.Time `gorm:"autoCreateTime"`
    UpdatedAt     time.Time `gorm:"autoUpdateTime"`
}

type Coupon struct {
    ID        string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    Code      string    `gorm:"uniqueIndex;not null"`
    Discount  float64   `gorm:"not null"`
    UsedBy    *string   `gorm:"type:uuid"`
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

type PaymentServiceServer struct {
    pb.UnimplementedPaymentServiceServer
    db    *gorm.DB
    redis *redis.Client
    nc    *nats.Conn
}

func NewPaymentServiceServer(db *gorm.DB, redis *redis.Client, nc *nats.Conn) *PaymentServiceServer {
    return &PaymentServiceServer{
        db:    db,
        redis: redis,
        nc:    nc,
    }
}

func (s *PaymentServiceServer) CreateSubscription(ctx context.Context, req *pb.CreateSubscriptionRequest) (*pb.Subscription, error) {
    // Rate limiting
    rateKey := "rate:subscription:" + req.UserId
    count, err := s.redis.Incr(ctx, rateKey).Result()
    if err == nil {
        if count > 3 {
            return nil, status.Error(codes.ResourceExhausted, "subscription creation limit exceeded")
        }
        if count == 1 {
            s.redis.Expire(ctx, rateKey, time.Hour)
        }
    }

    // Get plan details
    var plan PricingPlan
    if err := s.db.Where("id = ?", req.PlanId).First(&plan).Error; err != nil {
        return nil, status.Error(codes.NotFound, "plan not found")
    }

    // Check for existing active subscription
    var existing Subscription
    if err := s.db.Where("user_id = ? AND status = ?", req.UserId, "active").First(&existing).Error; err == nil {
        return nil, status.Error(codes.AlreadyExists, "user already has an active subscription")
    }

    // Create subscription
    subscription := &Subscription{
        ID:        uuid.New().String(),
        UserID:    req.UserId,
        PlanID:    plan.ID,
        PlanName:  plan.Name,
        Status:    "active",
        Price:     plan.Price,
        Currency:  plan.Currency,
        StartDate: time.Now(),
        EndDate:   time.Now().AddDate(0, 1, 0), // 1 month
    }

    if err := s.db.Create(subscription).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create subscription")
    }

    // Cache subscription in Redis
    cacheData := fmt.Sprintf(`{"id":"%s","status":"%s","end_date":%d}`, 
        subscription.ID, subscription.Status, subscription.EndDate.Unix())
    s.redis.Set(ctx, "sub:"+req.UserId, cacheData, 24*time.Hour)

    // Publish event
    event := fmt.Sprintf(`{"event":"subscription_created","subscription_id":"%s","user_id":"%s","plan":"%s","price":%.2f,"timestamp":%d}`,
        subscription.ID, req.UserId, plan.Name, plan.Price, time.Now().Unix())
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

func (s *PaymentServiceServer) CancelSubscription(ctx context.Context, req *pb.CancelSubscriptionRequest) (*pb.CancelSubscriptionResponse, error) {
    var subscription Subscription
    if err := s.db.Where("id = ? AND user_id = ?", req.SubscriptionId, req.UserId).First(&subscription).Error; err != nil {
        return nil, status.Error(codes.NotFound, "subscription not found")
    }

    if subscription.Status != "active" {
        return nil, status.Error(codes.FailedPrecondition, "subscription is not active")
    }

    // Cancel subscription
    subscription.Status = "cancelled"
    if err := s.db.Save(&subscription).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to cancel subscription")
    }

    // Invalidate cache
    s.redis.Del(ctx, "sub:"+req.UserId)

    // Publish event
    event := fmt.Sprintf(`{"event":"subscription_cancelled","subscription_id":"%s","user_id":"%s","timestamp":%d}`,
        subscription.ID, req.UserId, time.Now().Unix())
    s.nc.Publish("payment.events", []byte(event))

    return &pb.CancelSubscriptionResponse{Message: "Subscription cancelled successfully"}, nil
}

func (s *PaymentServiceServer) GetSubscription(ctx context.Context, req *pb.GetSubscriptionRequest) (*pb.Subscription, error) {
    // Check cache
    cached, err := s.redis.Get(ctx, "sub:"+req.UserId).Result()
    if err == nil {
        // Parse cached data (simplified)
        return &pb.Subscription{UserId: req.UserId, Status: "active"}, nil
    }

    var subscription Subscription
    if err := s.db.Where("user_id = ? AND status = ?", req.UserId, "active").First(&subscription).Error; err != nil {
        return nil, status.Error(codes.NotFound, "no active subscription found")
    }

    // Cache for future requests
    cacheData := fmt.Sprintf(`{"id":"%s","status":"%s","end_date":%d}`, 
        subscription.ID, subscription.Status, subscription.EndDate.Unix())
    s.redis.Set(ctx, "sub:"+req.UserId, cacheData, 5*time.Minute)

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

func (s *PaymentServiceServer) ProcessPayment(ctx context.Context, req *pb.ProcessPaymentRequest) (*pb.ProcessPaymentResponse, error) {
    // Rate limiting
    rateKey := "rate:payment:" + req.UserId
    count, err := s.redis.Incr(ctx, rateKey).Result()
    if err == nil {
        if count > 10 {
            return nil, status.Error(codes.ResourceExhausted, "payment processing limit exceeded")
        }
        if count == 1 {
            s.redis.Expire(ctx, rateKey, time.Hour)
        }
    }

    // Validate amount
    if req.Amount <= 0 {
        return nil, status.Error(codes.InvalidArgument, "invalid amount")
    }

    // Start transaction
    tx := s.db.Begin()

    // Create transaction record
    transaction := &PaymentTransaction{
        ID:            uuid.New().String(),
        UserID:        req.UserId,
        Amount:        req.Amount,
        Currency:      req.Currency,
        Status:        "pending",
        PaymentMethod: req.PaymentMethodId,
        Description:   req.Description,
    }

    if err := tx.Create(transaction).Error; err != nil {
        tx.Rollback()
        return nil, status.Error(codes.Internal, "failed to create transaction")
    }

    // Simulate payment processing with external gateway
    success, err := s.callPaymentGateway(transaction)
    if err != nil {
        transaction.Status = "failed"
        tx.Save(transaction)
        tx.Rollback()
        return nil, status.Error(codes.Internal, "payment gateway error")
    }

    if success {
        transaction.Status = "completed"
        transaction.ReceiptURL = fmt.Sprintf("https://payments.musicstreaming.com/receipts/%s.pdf", transaction.ID)
        
        if err := tx.Save(transaction).Error; err != nil {
            tx.Rollback()
            return nil, status.Error(codes.Internal, "failed to update transaction")
        }

        // Update or extend subscription
        var subscription Subscription
        if err := tx.Where("user_id = ? AND status = ?", req.UserId, "active").First(&subscription).Error; err == nil {
            // Extend existing subscription
            subscription.EndDate = subscription.EndDate.AddDate(0, 1, 0)
            if err := tx.Save(&subscription).Error; err != nil {
                tx.Rollback()
                return nil, status.Error(codes.Internal, "failed to update subscription")
            }
        } else {
            // Create new subscription (for first payment)
            subscription = Subscription{
                ID:        uuid.New().String(),
                UserID:    req.UserId,
                PlanID:    "premium",
                PlanName:  "Premium",
                Status:    "active",
                Price:     req.Amount,
                Currency:  req.Currency,
                StartDate: time.Now(),
                EndDate:   time.Now().AddDate(0, 1, 0),
            }
            if err := tx.Create(&subscription).Error; err != nil {
                tx.Rollback()
                return nil, status.Error(codes.Internal, "failed to create subscription")
            }
        }

        tx.Commit()

        // Invalidate cache
        s.redis.Del(ctx, "sub:"+req.UserId)

        // Publish event
        event := fmt.Sprintf(`{"event":"payment_completed","transaction_id":"%s","user_id":"%s","amount":%.2f,"currency":"%s","timestamp":%d}`,
            transaction.ID, req.UserId, req.Amount, req.Currency, time.Now().Unix())
        s.nc.Publish("payment.events", []byte(event))

        return &pb.ProcessPaymentResponse{
            TransactionId: transaction.ID,
            Success:       true,
            Message:       "Payment processed successfully",
            ReceiptUrl:    transaction.ReceiptURL,
        }, nil
    }

    transaction.Status = "failed"
    tx.Save(transaction)
    tx.Rollback()

    return &pb.ProcessPaymentResponse{
        Success: false,
        Message: "Payment processing failed",
    }, nil
}

func (s *PaymentServiceServer) callPaymentGateway(transaction *PaymentTransaction) (bool, error) {
    // Simulate external payment gateway call
    // In production, integrate with Stripe, PayPal, etc.
    time.Sleep(500 * time.Millisecond)
    
    // Simulate 95% success rate
    if time.Now().UnixNano()%100 < 95 {
        return true, nil
    }
    return false, fmt.Errorf("payment gateway declined the transaction")
}

func (s *PaymentServiceServer) GetPaymentHistory(ctx context.Context, req *pb.GetPaymentHistoryRequest) (*pb.PaymentHistoryResponse, error) {
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

    if err := s.db.Where("user_id = ?", req.UserId).
        Offset(int(offset)).Limit(int(req.PageSize)).
        Order("created_at DESC").
        Find(&transactions).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get payment history")
    }

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

    return &pb.PaymentHistoryResponse{
        Transactions: pbTransactions,
        Total:        int32(total),
    }, nil
}

func (s *PaymentServiceServer) ApplyCoupon(ctx context.Context, req *pb.ApplyCouponRequest) (*pb.ApplyCouponResponse, error) {
    var coupon Coupon
    if err := s.db.Where("code = ? AND expires_at > ?", req.CouponCode, time.Now()).First(&coupon).Error; err != nil {
        return nil, status.Error(codes.NotFound, "invalid or expired coupon")
    }

    if coupon.UsedBy != nil {
        return nil, status.Error(codes.AlreadyExists, "coupon already used")
    }

    // Mark coupon as used
    coupon.UsedBy = &req.UserId
    now := time.Now()
    coupon.UsedAt = &now
    
    if err := s.db.Save(&coupon).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to apply coupon")
    }

    return &pb.ApplyCouponResponse{
        Discount: coupon.Discount,
        Message:  fmt.Sprintf("Coupon applied! You saved %.2f", coupon.Discount),
        NewTotal: 0, // Would calculate based on original amount
    }, nil
}

func (s *PaymentServiceServer) GetInvoice(ctx context.Context, req *pb.GetInvoiceRequest) (*pb.Invoice, error) {
    var transaction PaymentTransaction
    if err := s.db.Where("id = ? AND user_id = ?", req.TransactionId, req.UserId).First(&transaction).Error; err != nil {
        return nil, status.Error(codes.NotFound, "transaction not found")
    }

    return &pb.Invoice{
        InvoiceId: transaction.ID,
        UserId:    transaction.UserID,
        Amount:    transaction.Amount,
        Currency:  transaction.Currency,
        PdfUrl:    fmt.Sprintf("https://payments.musicstreaming.com/invoices/%s.pdf", transaction.ID),
        Status:    transaction.Status,
        IssuedAt:  transaction.CreatedAt.Unix(),
    }, nil
}

func (s *PaymentServiceServer) GetPricingPlans(ctx context.Context, req *pb.GetPricingPlansRequest) (*pb.GetPricingPlansResponse, error) {
    var plans []PricingPlan
    if err := s.db.Find(&plans).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to get pricing plans")
    }

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

func (s *PaymentServiceServer) UpdatePaymentMethod(ctx context.Context, req *pb.UpdatePaymentMethodRequest) (*pb.UpdatePaymentMethodResponse, error) {
    // Validate card details (basic validation)
    if len(req.CardNumber) < 13 || len(req.CardNumber) > 19 {
        return nil, status.Error(codes.InvalidArgument, "invalid card number")
    }
    if len(req.CardExpiry) != 5 || req.CardExpiry[2] != '/' {
        return nil, status.Error(codes.InvalidArgument, "invalid expiry format (MM/YY)")
    }
    if len(req.CardCvv) != 3 && len(req.CardCvv) != 4 {
        return nil, status.Error(codes.InvalidArgument, "invalid CVV")
    }

    // In production, tokenize card with payment provider
    paymentMethodID := "pm_" + uuid.New().String()
    
    // Store payment method in Redis with encryption (simplified)
    paymentData := fmt.Sprintf(`{"last4":"%s","expiry":"%s"}`, 
        req.CardNumber[len(req.CardNumber)-4:], req.CardExpiry)
    s.redis.Set(ctx, "pm:"+req.UserId, paymentData, 365*24*time.Hour)

    return &pb.UpdatePaymentMethodResponse{
        PaymentMethodId: paymentMethodID,
        Message:         "Payment method updated successfully",
    }, nil
}

func (s *PaymentServiceServer) GetPaymentMethod(ctx context.Context, req *pb.GetPaymentMethodRequest) (*pb.PaymentMethod, error) {
    cached, err := s.redis.Get(ctx, "pm:"+req.UserId).Result()
    if err != nil {
        return nil, status.Error(codes.NotFound, "no payment method found")
    }

    return &pb.PaymentMethod{
        Id:     "pm_default",
        Last4:  cached[len(cached)-4:],
        CardType: "Visa",
    }, nil
}

func main() {
    // Get configuration
    dbHost := getEnv("DB_HOST", "postgres")
    dbPort := getEnv("DB_PORT", "5432")
    dbUser := getEnv("DB_USER", "music_user")
    dbPass := getEnv("DB_PASSWORD", "music_password")
    dbName := getEnv("DB_NAME", "music_db")
    redisAddr := getEnv("REDIS_ADDR", "redis:6379")
    natsURL := getEnv("NATS_URL", "nats://nats:4222")

    // Connect to database
    dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
        dbHost, dbUser, dbPass, dbName, dbPort)
    
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatalf("Failed to connect to database: %v", err)
    }

    // Auto migrate
    if err := db.AutoMigrate(&Subscription{}, &PaymentTransaction{}, &Coupon{}, &PricingPlan{}); err != nil {
        log.Fatalf("Failed to migrate database: %v", err)
    }

    // Seed initial data
    seedDatabase(db)

    // Connect to Redis
    redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
    if err := redisClient.Ping(context.Background()).Err(); err != nil {
        log.Fatalf("Failed to connect to Redis: %v", err)
    }

    // Connect to NATS
    nc, err := nats.Connect(natsURL)
    if err != nil {
        log.Fatalf("Failed to connect to NATS: %v", err)
    }
    defer nc.Close()

    // Create gRPC server
    grpcServer := grpc.NewServer(
        grpc.UnaryInterceptor(grpcLoggingInterceptor),
    )

    // Register services
    paymentServer := NewPaymentServiceServer(db, redisClient, nc)
    pb.RegisterPaymentServiceServer(grpcServer, paymentServer)

    // Register health check
    healthServer := health.NewServer()
    grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
    healthServer.SetServingStatus("payment-service", grpc_health_v1.HealthCheckResponse_SERVING)

    // Start listening
    lis, err := net.Listen("tcp", ":50053")
    if err != nil {
        log.Fatalf("Failed to listen: %v", err)
    }

    log.Println("Payment service running on port 50053")
    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("Failed to serve: %v", err)
    }
}

func seedDatabase(db *gorm.DB) {
    // Check if plans already exist
    var count int64
    db.Model(&PricingPlan{}).Count(&count)
    if count > 0 {
        return
    }

    // Seed pricing plans
    plans := []PricingPlan{
        {Name: "Free", Price: 0, Currency: "USD", Interval: "month", Quality: 128, OfflineMode: false},
        {Name: "Premium", Price: 9.99, Currency: "USD", Interval: "month", Quality: 320, OfflineMode: true},
        {Name: "Family", Price: 14.99, Currency: "USD", Interval: "month", Quality: 320, OfflineMode: true},
        {Name: "Student", Price: 4.99, Currency: "USD", Interval: "month", Quality: 320, OfflineMode: true},
        {Name: "Annual", Price: 99.99, Currency: "USD", Interval: "year", Quality: 320, OfflineMode: true},
    }

    for _, plan := range plans {
        db.Create(&plan)
    }

    // Seed coupons
    coupons := []Coupon{
        {Code: "WELCOME20", Discount: 20, ExpiresAt: time.Now().AddDate(1, 0, 0)},
        {Code: "SAVE10", Discount: 10, ExpiresAt: time.Now().AddDate(0, 6, 0)},
        {Code: "FLASH50", Discount: 50, ExpiresAt: time.Now().AddDate(0, 0, 7)},
    }

    for _, coupon := range coupons {
        db.Create(&coupon)
    }

    log.Println("Database seeded with pricing plans and coupons")
}

func grpcLoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
    start := time.Now()
    resp, err := handler(ctx, req)
    log.Printf("Method: %s, Duration: %v, Error: %v", info.FullMethod, time.Since(start), err)
    return resp, err
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
