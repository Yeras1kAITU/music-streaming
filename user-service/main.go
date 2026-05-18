package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "os"
    "time"

    "github.com/go-redis/redis/v8"
    "github.com/golang-jwt/jwt/v5"
    "github.com/google/uuid"
    "github.com/nats-io/nats.go"
    "golang.org/x/crypto/bcrypt"
    "google.golang.org/grpc"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/health"
    "google.golang.org/grpc/health/grpc_health_v1"
    "google.golang.org/grpc/status"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"

    pb "github.com/music-streaming/proto/user"
)

type User struct {
    ID        string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    Email     string    `gorm:"uniqueIndex;not null"`
    Password  string    `gorm:"not null"`
    Username  string    `gorm:"not null"`
    Role      string    `gorm:"default:user"`
    Verified  bool      `gorm:"default:false"`
    CreatedAt time.Time `gorm:"autoCreateTime"`
    UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

type Session struct {
    ID           string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    UserID       string    `gorm:"type:uuid;not null;index"`
    Token        string    `gorm:"uniqueIndex;not null"`
    RefreshToken string    `gorm:"uniqueIndex"`
    ExpiresAt    time.Time `gorm:"not null"`
    CreatedAt    time.Time `gorm:"autoCreateTime"`
}

type UserServiceServer struct {
    pb.UnimplementedUserServiceServer
    db          *gorm.DB
    redis       *redis.Client
    nc          *nats.Conn
    jwtSecret   []byte
}

func NewUserServiceServer(db *gorm.DB, redis *redis.Client, nc *nats.Conn, jwtSecret string) *UserServiceServer {
    return &UserServiceServer{
        db:        db,
        redis:     redis,
        nc:        nc,
        jwtSecret: []byte(jwtSecret),
    }
}

func (s *UserServiceServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
    // Check if user exists
    var existingUser User
    if err := s.db.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
        return nil, status.Error(codes.AlreadyExists, "user already exists")
    }

    // Hash password
    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to hash password")
    }

    // Create user
    user := &User{
        Email:    req.Email,
        Password: string(hashedPassword),
        Username: req.Username,
        Role:     "user",
        Verified: false,
    }

    if err := s.db.Create(user).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create user")
    }

    // Publish user registered event to NATS
    event := fmt.Sprintf(`{"event":"user_registered","user_id":"%s","email":"%s","username":"%s","timestamp":%d}`,
        user.ID, user.Email, user.Username, time.Now().Unix())
    if err := s.nc.Publish("user.events", []byte(event)); err != nil {
        log.Printf("Failed to publish user_registered event: %v", err)
    }

    // Store verification token in Redis (expires in 24 hours)
    verifyToken := uuid.New().String()
    if err := s.redis.Set(ctx, "verify:"+user.ID, verifyToken, 24*time.Hour).Err(); err != nil {
        log.Printf("Failed to store verification token: %v", err)
    }

    return &pb.RegisterResponse{
        UserId:  user.ID,
        Message: "User registered successfully. Please verify your email.",
    }, nil
}

func (s *UserServiceServer) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
    // Rate limiting check
    rateKey := "rate:login:" + req.Email
    count, err := s.redis.Incr(ctx, rateKey).Result()
    if err == nil {
        if count > 5 {
            return nil, status.Error(codes.ResourceExhausted, "too many login attempts")
        }
        if count == 1 {
            s.redis.Expire(ctx, rateKey, time.Minute)
        }
    }

    // Find user
    var user User
    if err := s.db.Where("email = ?", req.Email).First(&user).Error; err != nil {
        return nil, status.Error(codes.NotFound, "invalid credentials")
    }

    // Verify password
    if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
        // Increment failed attempts
        s.redis.Incr(ctx, "failed:"+req.Email)
        return nil, status.Error(codes.Unauthenticated, "invalid credentials")
    }

    // Check if email is verified
    if !user.Verified {
        return nil, status.Error(codes.PermissionDenied, "email not verified")
    }

    // Generate JWT token
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
        "user_id": user.ID,
        "email":   user.Email,
        "exp":     time.Now().Add(24 * time.Hour).Unix(),
    })

    tokenString, err := token.SignedString(s.jwtSecret)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to generate token")
    }

    // Generate refresh token
    refreshToken := uuid.New().String()

    // Store session
    session := &Session{
        UserID:       user.ID,
        Token:        tokenString,
        RefreshToken: refreshToken,
        ExpiresAt:    time.Now().Add(24 * time.Hour),
    }
    if err := s.db.Create(session).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to create session")
    }

    // Cache session in Redis
    sessionData := fmt.Sprintf(`{"user_id":"%s","expires_at":%d}`, user.ID, session.ExpiresAt.Unix())
    if err := s.redis.Set(ctx, "session:"+tokenString, sessionData, 24*time.Hour).Err(); err != nil {
        log.Printf("Failed to cache session: %v", err)
    }

    // Reset failed attempts
    s.redis.Del(ctx, "failed:"+req.Email)
    s.redis.Del(ctx, rateKey)

    return &pb.LoginResponse{
        Token:        tokenString,
        UserId:       user.ID,
        RefreshToken: refreshToken,
    }, nil
}

func (s *UserServiceServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.User, error) {
    // Check cache first
    cached, err := s.redis.Get(ctx, "user:"+req.UserId).Result()
    if err == nil {
        return &pb.User{
            Id:       req.UserId,
            Email:    cached,
            Username: cached,
        }, nil
    }

    var user User
    if err := s.db.First(&user, "id = ?", req.UserId).Error; err != nil {
        return nil, status.Error(codes.NotFound, "user not found")
    }

    // Cache user data
    s.redis.Set(ctx, "user:"+user.ID, user.Email, 30*time.Minute)

    return &pb.User{
        Id:        user.ID,
        Email:     user.Email,
        Username:  user.Username,
        Role:      user.Role,
        Verified:  user.Verified,
        CreatedAt: user.CreatedAt.Unix(),
    }, nil
}

func (s *UserServiceServer) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.User, error) {
    var user User
    if err := s.db.First(&user, "id = ?", req.UserId).Error; err != nil {
        return nil, status.Error(codes.NotFound, "user not found")
    }

    updates := make(map[string]interface{})
    if req.Username != "" {
        updates["username"] = req.Username
    }
    if req.Email != "" {
        updates["email"] = req.Email
    }
    updates["updated_at"] = time.Now()

    if err := s.db.Model(&user).Updates(updates).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update user")
    }

    // Invalidate cache
    s.redis.Del(ctx, "user:"+user.ID)

    return &pb.User{
        Id:        user.ID,
        Email:     user.Email,
        Username:  user.Username,
        Role:      user.Role,
        Verified:  user.Verified,
        CreatedAt: user.CreatedAt.Unix(),
    }, nil
}

func (s *UserServiceServer) DeleteUser(ctx context.Context, req *pb.DeleteUserRequest) (*pb.DeleteUserResponse, error) {
    // Delete user
    if err := s.db.Delete(&User{}, "id = ?", req.UserId).Error; err != nil {
        return nil, status.Error(codes.NotFound, "user not found")
    }

    // Delete all sessions
    s.db.Delete(&Session{}, "user_id = ?", req.UserId)

    // Invalidate cache
    s.redis.Del(ctx, "user:"+req.UserId)

    // Publish user deleted event
    event := fmt.Sprintf(`{"event":"user_deleted","user_id":"%s","timestamp":%d}`, req.UserId, time.Now().Unix())
    s.nc.Publish("user.events", []byte(event))

    return &pb.DeleteUserResponse{Message: "User deleted successfully"}, nil
}

func (s *UserServiceServer) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
    // Check Redis cache first
    cached, err := s.redis.Get(ctx, "session:"+req.Token).Result()
    if err == nil {
        return &pb.ValidateTokenResponse{Valid: true}, nil
    }

    // Check database
    var session Session
    if err := s.db.Where("token = ? AND expires_at > ?", req.Token, time.Now()).First(&session).Error; err != nil {
        return &pb.ValidateTokenResponse{Valid: false}, nil
    }

    // Parse and validate JWT
    parsedToken, err := jwt.Parse(req.Token, func(token *jwt.Token) (interface{}, error) {
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method")
        }
        return s.jwtSecret, nil
    })

    if err != nil || !parsedToken.Valid {
        return &pb.ValidateTokenResponse{Valid: false}, nil
    }

    // Cache for future requests
    sessionData := fmt.Sprintf(`{"user_id":"%s"}`, session.UserID)
    s.redis.Set(ctx, "session:"+req.Token, sessionData, time.Hour)

    return &pb.ValidateTokenResponse{
        UserId: session.UserID,
        Valid:  true,
    }, nil
}

func (s *UserServiceServer) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.LogoutResponse, error) {
    // Delete from database
    if err := s.db.Delete(&Session{}, "token = ?", req.Token).Error; err != nil {
        log.Printf("Failed to delete session: %v", err)
    }

    // Delete from Redis
    s.redis.Del(ctx, "session:"+req.Token)

    return &pb.LogoutResponse{Message: "Logged out successfully"}, nil
}

func (s *UserServiceServer) ChangePassword(ctx context.Context, req *pb.ChangePasswordRequest) (*pb.ChangePasswordResponse, error) {
    var user User
    if err := s.db.First(&user, "id = ?", req.UserId).Error; err != nil {
        return nil, status.Error(codes.NotFound, "user not found")
    }

    // Verify old password
    if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
        return nil, status.Error(codes.Unauthenticated, "invalid old password")
    }

    // Hash new password
    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to hash password")
    }

    // Update password
    if err := s.db.Model(&user).Update("password", string(hashedPassword)).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to update password")
    }

    // Delete all sessions for this user (force re-login)
    s.db.Delete(&Session{}, "user_id = ?", req.UserId)

    return &pb.ChangePasswordResponse{Message: "Password changed successfully"}, nil
}

func (s *UserServiceServer) VerifyEmail(ctx context.Context, req *pb.VerifyEmailRequest) (*pb.VerifyEmailResponse, error) {
    // Check verification token
    storedToken, err := s.redis.Get(ctx, "verify:"+req.UserId).Result()
    if err != nil || storedToken != req.Token {
        return &pb.VerifyEmailResponse{Success: false, Message: "Invalid or expired verification token"}, nil
    }

    // Update user
    if err := s.db.Model(&User{}).Where("id = ?", req.UserId).Update("verified", true).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to verify email")
    }

    // Delete verification token
    s.redis.Del(ctx, "verify:"+req.UserId)

    return &pb.VerifyEmailResponse{Success: true, Message: "Email verified successfully"}, nil
}

func (s *UserServiceServer) ForgotPassword(ctx context.Context, req *pb.ForgotPasswordRequest) (*pb.ForgotPasswordResponse, error) {
    var user User
    if err := s.db.Where("email = ?", req.Email).First(&user).Error; err != nil {
        // Don't reveal if user exists
        return &pb.ForgotPasswordResponse{Message: "If the email exists, a reset link will be sent"}, nil
    }

    // Generate reset token
    resetToken := uuid.New().String()
    s.redis.Set(ctx, "reset:"+user.ID, resetToken, 1*time.Hour)

    // In production, send email here
    log.Printf("Password reset token for %s: %s", req.Email, resetToken)

    return &pb.ForgotPasswordResponse{Message: "If the email exists, a reset link will be sent"}, nil
}

func (s *UserServiceServer) ResetPassword(ctx context.Context, req *pb.ResetPasswordRequest) (*pb.ResetPasswordResponse, error) {
    // Find user by reset token
    var userID string
    keys, err := s.redis.Keys(ctx, "reset:*").Result()
    if err == nil {
        for _, key := range keys {
            token, _ := s.redis.Get(ctx, key).Result()
            if token == req.Token {
                userID = key[len("reset:"):]
                break
            }
        }
    }

    if userID == "" {
        return nil, status.Error(codes.InvalidArgument, "invalid or expired reset token")
    }

    // Hash new password
    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
    if err != nil {
        return nil, status.Error(codes.Internal, "failed to hash password")
    }

    // Update password
    if err := s.db.Model(&User{}).Where("id = ?", userID).Update("password", string(hashedPassword)).Error; err != nil {
        return nil, status.Error(codes.Internal, "failed to reset password")
    }

    // Delete reset token
    s.redis.Del(ctx, "reset:"+userID)

    return &pb.ResetPasswordResponse{Message: "Password reset successfully"}, nil
}

func (s *UserServiceServer) GetUserStats(ctx context.Context, req *pb.GetUserStatsRequest) (*pb.UserStats, error) {
    var totalPlaylists int64
    var totalTracks int64
    var totalPlays int64

    // Get counts from database
    s.db.Table("playlists").Where("user_id = ?", req.UserId).Count(&totalPlaylists)
    s.db.Table("tracks").Where("user_id = ?", req.UserId).Count(&totalTracks)
    s.db.Table("tracks").Where("user_id = ?", req.UserId).Select("COALESCE(SUM(plays), 0)").Scan(&totalPlays)

    // Get subscription info from Redis or database
    subscriptionDaysLeft := int64(0)
    subKey := "subscription:" + req.UserId
    if days, err := s.redis.Get(ctx, subKey).Int64(); err == nil {
        subscriptionDaysLeft = days
    }

    return &pb.UserStats{
        TotalPlaylists:       int32(totalPlaylists),
        TotalTracksUploaded:  int32(totalTracks),
        TotalPlays:           int32(totalPlays),
        SubscriptionDaysLeft: subscriptionDaysLeft,
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
    jwtSecret := getEnv("JWT_SECRET", "default-secret-key-change-in-production")

    // Connect to database
    dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
        dbHost, dbUser, dbPass, dbName, dbPort)
    
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Fatalf("Failed to connect to database: %v", err)
    }

    // Auto migrate
    if err := db.AutoMigrate(&User{}, &Session{}); err != nil {
        log.Fatalf("Failed to migrate database: %v", err)
    }

    // Connect to Redis
    redisClient := redis.NewClient(&redis.Options{
        Addr: redisAddr,
        DB:   0,
    })
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
        grpc.MaxConcurrentStreams(1000),
    )

    // Register services
    userServer := NewUserServiceServer(db, redisClient, nc, jwtSecret)
    pb.RegisterUserServiceServer(grpcServer, userServer)

    // Register health check
    healthServer := health.NewServer()
    grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
    healthServer.SetServingStatus("user-service", grpc_health_v1.HealthCheckResponse_SERVING)

    // Start listening
    lis, err := net.Listen("tcp", ":50051")
    if err != nil {
        log.Fatalf("Failed to listen: %v", err)
    }

    log.Println("User service running on port 50051")
    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("Failed to serve: %v", err)
    }
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
