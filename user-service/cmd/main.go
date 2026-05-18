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
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

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

type userServiceServer struct {
	pb.UnimplementedUserServiceServer
	db        *gorm.DB
	redis     *redis.Client
	nc        *nats.Conn
	jwtSecret []byte
}

func NewUserServiceServer(db *gorm.DB, redis *redis.Client, nc *nats.Conn, jwtSecret string) *userServiceServer {
	return &userServiceServer{
		db:        db,
		redis:     redis,
		nc:        nc,
		jwtSecret: []byte(jwtSecret),
	}
}

func (s *userServiceServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	var existingUser User
	if err := s.db.Where("email = ?", req.Email).First(&existingUser).Error; err == nil {
		return nil, status.Error(codes.AlreadyExists, "user already exists")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to hash password")
	}

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

	// Publish event
	event := fmt.Sprintf(`{"event":"user_registered","user_id":"%s","email":"%s"}`, user.ID, user.Email)
	s.nc.Publish("user.events", []byte(event))

	return &pb.RegisterResponse{
		UserId:  user.ID,
		Message: "User registered successfully. Please verify your email.",
	}, nil
}

func (s *userServiceServer) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	var user User
	if err := s.db.Where("email = ?", req.Email).First(&user).Error; err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate token")
	}

	refreshToken := uuid.New().String()

	session := &Session{
		UserID:       user.ID,
		Token:        tokenString,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}
	s.db.Create(session)

	return &pb.LoginResponse{
		Token:        tokenString,
		UserId:       user.ID,
		RefreshToken: refreshToken,
	}, nil
}

func (s *userServiceServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.User, error) {
	var user User
	if err := s.db.First(&user, "id = ?", req.UserId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	return &pb.User{
		Id:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		Role:      user.Role,
		Verified:  user.Verified,
		CreatedAt: user.CreatedAt.Unix(),
	}, nil
}

func (s *userServiceServer) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.User, error) {
	var user User
	if err := s.db.First(&user, "id = ?", req.UserId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	if req.Username != "" {
		user.Username = req.Username
	}
	if req.Email != "" {
		user.Email = req.Email
	}

	s.db.Save(&user)

	return &pb.User{
		Id:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		Role:      user.Role,
		Verified:  user.Verified,
		CreatedAt: user.CreatedAt.Unix(),
	}, nil
}

func (s *userServiceServer) DeleteUser(ctx context.Context, req *pb.DeleteUserRequest) (*pb.DeleteUserResponse, error) {
	s.db.Delete(&User{}, "id = ?", req.UserId)
	s.db.Delete(&Session{}, "user_id = ?", req.UserId)
	return &pb.DeleteUserResponse{Message: "User deleted successfully"}, nil
}

func (s *userServiceServer) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	var session Session
	if err := s.db.Where("token = ? AND expires_at > ?", req.Token, time.Now()).First(&session).Error; err != nil {
		return &pb.ValidateTokenResponse{Valid: false}, nil
	}

	parsedToken, err := jwt.Parse(req.Token, func(token *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})

	if err != nil || !parsedToken.Valid {
		return &pb.ValidateTokenResponse{Valid: false}, nil
	}

	return &pb.ValidateTokenResponse{
		UserId: session.UserID,
		Valid:  true,
	}, nil
}

func (s *userServiceServer) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	s.db.Delete(&Session{}, "token = ?", req.Token)
	return &pb.LogoutResponse{Message: "Logged out successfully"}, nil
}

func (s *userServiceServer) ChangePassword(ctx context.Context, req *pb.ChangePasswordRequest) (*pb.ChangePasswordResponse, error) {
	var user User
	if err := s.db.First(&user, "id = ?", req.UserId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid old password")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to hash password")
	}

	s.db.Model(&user).Update("password", string(hashedPassword))
	s.db.Delete(&Session{}, "user_id = ?", req.UserId)

	return &pb.ChangePasswordResponse{Message: "Password changed successfully"}, nil
}

func (s *userServiceServer) VerifyEmail(ctx context.Context, req *pb.VerifyEmailRequest) (*pb.VerifyEmailResponse, error) {
	s.db.Model(&User{}).Where("id = ?", req.UserId).Update("verified", true)
	return &pb.VerifyEmailResponse{Success: true, Message: "Email verified successfully"}, nil
}

func (s *userServiceServer) ForgotPassword(ctx context.Context, req *pb.ForgotPasswordRequest) (*pb.ForgotPasswordResponse, error) {
	return &pb.ForgotPasswordResponse{Message: "If the email exists, a reset link will be sent"}, nil
}

func (s *userServiceServer) ResetPassword(ctx context.Context, req *pb.ResetPasswordRequest) (*pb.ResetPasswordResponse, error) {
	return &pb.ResetPasswordResponse{Message: "Password reset successfully"}, nil
}

func (s *userServiceServer) GetUserStats(ctx context.Context, req *pb.GetUserStatsRequest) (*pb.UserStats, error) {
	return &pb.UserStats{
		TotalPlaylists:       0,
		TotalTracksUploaded:  0,
		TotalPlays:           0,
		SubscriptionDaysLeft: 0,
	}, nil
}

func main() {
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Println("Metrics server listening on :9090")
		if err := http.ListenAndServe(":9090", nil); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()
	
	dbHost := getEnv("DB_HOST", "postgres")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "music_user")
	dbPass := getEnv("DB_PASSWORD", "music_password")
	dbName := getEnv("DB_NAME", "music_db")
	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	natsURL := getEnv("NATS_URL", "nats://nats:4222")
	jwtSecret := getEnv("JWT_SECRET", "default-secret-key-change-in-production")

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=UTC",
		dbHost, dbUser, dbPass, dbName, dbPort)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	db.AutoMigrate(&User{}, &Session{})

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterUserServiceServer(grpcServer, NewUserServiceServer(db, redisClient, nc, jwtSecret))

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("user-service", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Println("User service running on port 50051")

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down user service...")
	grpcServer.GracefulStop()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
