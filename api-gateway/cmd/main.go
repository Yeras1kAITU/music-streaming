package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/music-streaming/api-gateway/internal/handler"
	"github.com/music-streaming/api-gateway/internal/middleware"
	pb "github.com/music-streaming/proto/music"
	paymentpb "github.com/music-streaming/proto/payment"
	userpb "github.com/music-streaming/proto/user"
)

func main() {

	userServiceAddr := getEnv("USER_SERVICE_ADDR", "user-service:50051")
	musicServiceAddr := getEnv("MUSIC_SERVICE_ADDR", "music-service:50052")
	paymentServiceAddr := getEnv("PAYMENT_SERVICE_ADDR", "payment-service:50053")
	redisAddr := getEnv("REDIS_ADDR", "redis:6379")

	userConn, err := grpc.Dial(
		userServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50*1024*1024)),
	)
	if err != nil {
		log.Fatalf("Failed to connect to user service: %v", err)
	}
	defer userConn.Close()

	musicConn, err := grpc.Dial(
		musicServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(100*1024*1024)),
	)
	if err != nil {
		log.Fatalf("Failed to connect to music service: %v", err)
	}
	defer musicConn.Close()

	paymentConn, err := grpc.Dial(
		paymentServiceAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("Failed to connect to payment service: %v", err)
	}
	defer paymentConn.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	userClient := userpb.NewUserServiceClient(userConn)
	musicClient := pb.NewMusicServiceClient(musicConn)
	paymentClient := paymentpb.NewPaymentServiceClient(paymentConn)

	userHandler := handler.NewUserHandler(userClient)
	musicHandler := handler.NewMusicHandler(musicClient)
	paymentHandler := handler.NewPaymentHandler(paymentClient)

	authMiddleware := middleware.NewAuthMiddleware(userClient, redisClient)

	router := gin.Default()

	// ONLY ONE /metrics route - keep this one
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	router.Use(cors.New(cors.Config{
		AllowOrigins: []string{
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"http://localhost:3001",
			"http://localhost:3002",
			"http://localhost:63342",
			"http://127.0.0.1:63342",
			"http://localhost:5500",
			"http://127.0.0.1:5500",
		},
		AllowMethods: []string{
			"GET",
			"POST",
			"PUT",
			"PATCH",
			"DELETE",
			"OPTIONS",
		},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Content-Length",
			"Accept-Encoding",
			"X-CSRF-Token",
			"Authorization",
			"Accept",
			"Range",
		},
		ExposeHeaders: []string{
			"Content-Length",
			"Content-Range",
			"Accept-Ranges",
		},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
		})
	})

	// DELETE THIS ENTIRE BLOCK (lines 140-144 in your original)
	// router.GET("/metrics", func(c *gin.Context) {
	//     c.JSON(http.StatusOK, gin.H{
	//         "status": "metrics-ok",
	//     })
	// })

	auth := router.Group("/auth")
	{
		auth.POST("/register", userHandler.Register)
		auth.POST("/login", userHandler.Login)
		auth.POST("/verify", userHandler.VerifyEmail)
		auth.POST("/forgot-password", userHandler.ForgotPassword)
		auth.POST("/reset-password", userHandler.ResetPassword)
	}

	router.GET("/stream/:id", musicHandler.StreamTrack)

	router.GET("/api/pricing-plans", paymentHandler.GetPricingPlans)

	api := router.Group("/api")
	api.Use(authMiddleware.Authenticate())
	{
		api.GET("/user/profile", userHandler.GetProfile)
		api.PUT("/user/profile", userHandler.UpdateProfile)
		api.POST("/user/change-password", userHandler.ChangePassword)
		api.POST("/user/logout", userHandler.Logout)

		api.POST("/tracks/upload", musicHandler.UploadTrack)
		api.GET("/tracks/:id", musicHandler.GetTrack)
		api.GET("/tracks", musicHandler.ListTracks)
		api.GET("/tracks/search", musicHandler.SearchTracks)
		api.POST("/tracks/:id/like", musicHandler.LikeTrack)

		api.POST("/playlists", musicHandler.CreatePlaylist)
		api.GET("/playlists", musicHandler.GetUserPlaylists)
		api.GET("/playlists/:id", musicHandler.GetPlaylist)
		api.POST("/playlists/:id/tracks", musicHandler.AddToPlaylist)
		api.DELETE("/playlists/:id/tracks/:trackId", musicHandler.RemoveFromPlaylist)

		api.POST("/queue/add", musicHandler.AddToQueue)
		api.GET("/queue", musicHandler.GetQueue)

		api.GET("/recommendations", musicHandler.GetRecommendations)

		api.GET("/subscription", paymentHandler.GetSubscription)
		api.POST("/subscription", paymentHandler.CreateSubscription)
		api.DELETE("/subscription/:id", paymentHandler.CancelSubscription)

		api.POST("/payments/process", paymentHandler.ProcessPayment)
		api.GET("/payments/history", paymentHandler.GetPaymentHistory)

		api.POST("/coupons/apply", paymentHandler.ApplyCoupon)
	}

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		log.Println("API Gateway running on :8080")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)

	signal.Notify(
		quit,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	<-quit

	log.Println("Shutting down API Gateway...")

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("API Gateway stopped")
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}
