package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	pb "github.com/music-streaming/proto/music"
	paymentpb "github.com/music-streaming/proto/payment"
	userpb "github.com/music-streaming/proto/user"
)

type Gateway struct {
	userService    userpb.UserServiceClient
	musicService   pb.MusicServiceClient
	paymentService paymentpb.PaymentServiceClient
	redis          *redis.Client
	router         *gin.Engine
}

func main() {
	redisClient := redis.NewClient(&redis.Options{
		Addr: os.Getenv("REDIS_ADDR"),
	})

	userConn, err := grpc.Dial(os.Getenv("USER_SERVICE_ADDR"), grpc.WithInsecure())
	if err != nil {
		log.Fatal("Failed to connect to user service:", err)
	}

	musicConn, err := grpc.Dial(os.Getenv("MUSIC_SERVICE_ADDR"), grpc.WithInsecure())
	if err != nil {
		log.Fatal("Failed to connect to music service:", err)
	}

	paymentConn, err := grpc.Dial(os.Getenv("PAYMENT_SERVICE_ADDR"), grpc.WithInsecure())
	if err != nil {
		log.Fatal("Failed to connect to payment service:", err)
	}

	gateway := &Gateway{
		userService:    userpb.NewUserServiceClient(userConn),
		musicService:   pb.NewMusicServiceClient(musicConn),
		paymentService: paymentpb.NewPaymentServiceClient(paymentConn),
		redis:          redisClient,
	}

	gateway.setupRoutes()

	srv := &http.Server{
		Addr:    ":8080",
		Handler: gateway.router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start server:", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}
}

func (g *Gateway) setupRoutes() {
	g.router = gin.Default()

	g.router.POST("/register", g.register)
	g.router.POST("/login", g.login)
	g.router.POST("/logout", g.authMiddleware(), g.logout)

	api := g.router.Group("/api")
	api.Use(g.authMiddleware())
	{
		// User routes
		api.GET("/user/:id", g.getUser)
		api.PUT("/user/:id", g.updateUser)
		api.POST("/user/change-password", g.changePassword)

		// Music routes
		api.POST("/tracks/upload", g.uploadTrack)
		api.GET("/tracks/:id", g.getTrack)
		api.GET("/tracks", g.listTracks)
		api.GET("/tracks/search", g.searchTracks)
		api.POST("/playlists", g.createPlaylist)
		api.POST("/playlists/:id/tracks", g.addToPlaylist)
		api.DELETE("/playlists/:id/tracks/:trackId", g.removeFromPlaylist)
		api.GET("/playlists/:id", g.getPlaylist)
		api.GET("/recommendations", g.getRecommendations)

		// Payment routes
		api.POST("/subscriptions", g.createSubscription)
		api.DELETE("/subscriptions/:id", g.cancelSubscription)
		api.GET("/subscriptions", g.getSubscription)
		api.POST("/payments/process", g.processPayment)
		api.GET("/payments/history", g.getPaymentHistory)
		api.POST("/coupons/apply", g.applyCoupon)
		api.GET("/invoices/:transactionId", g.getInvoice)
	}
}

func (g *Gateway) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "No authorization token"})
			c.Abort()
			return
		}

		ctx := context.Background()
		resp, err := g.userService.ValidateToken(ctx, &userpb.ValidateTokenRequest{
			Token: token,
		})

		if err != nil || !resp.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		c.Set("user_id", resp.UserId)
		c.Next()
	}
}

func (g *Gateway) register(c *gin.Context) {
	var req userpb.RegisterRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := g.userService.Register(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) login(c *gin.Context) {
	var req userpb.LoginRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := g.userService.Login(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Store token in Redis with 24h expiration
	g.redis.Set(c.Request.Context(), "token:"+resp.Token, resp.UserId, 24*time.Hour)

	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) logout(c *gin.Context) {
	token := c.GetHeader("Authorization")

	g.redis.Del(c.Request.Context(), "token:"+token)

	resp, _ := g.userService.Logout(c.Request.Context(), &userpb.LogoutRequest{Token: token})
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) getUser(c *gin.Context) {
	userID := c.Param("id")
	resp, err := g.userService.GetUser(c.Request.Context(), &userpb.GetUserRequest{UserId: userID})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) updateUser(c *gin.Context) {
	var req userpb.UpdateUserRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = c.Param("id")

	resp, err := g.userService.UpdateUser(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) changePassword(c *gin.Context) {
	var req userpb.ChangePasswordRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = c.GetString("user_id")

	resp, err := g.userService.ChangePassword(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) uploadTrack(c *gin.Context) {
	// Handle file upload
	file, err := c.FormFile("audio")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()

	audioData := make([]byte, file.Size)
	src.Read(audioData)

	req := &pb.UploadTrackRequest{
		UserId:    c.GetString("user_id"),
		Title:     c.PostForm("title"),
		Artist:    c.PostForm("artist"),
		Album:     c.PostForm("album"),
		Duration:  0, // Would extract from metadata in production
		AudioData: audioData,
	}

	resp, err := g.musicService.UploadTrack(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) getTrack(c *gin.Context) {
	trackID := c.Param("id")
	resp, err := g.musicService.GetTrack(c.Request.Context(), &pb.GetTrackRequest{TrackId: trackID})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Track not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) listTracks(c *gin.Context) {
	resp, err := g.musicService.ListTracks(c.Request.Context(), &pb.ListTracksRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) searchTracks(c *gin.Context) {
	query := c.Query("q")
	resp, err := g.musicService.SearchTracks(c.Request.Context(), &pb.SearchTracksRequest{Query: query})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) createPlaylist(c *gin.Context) {
	var req pb.CreatePlaylistRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = c.GetString("user_id")

	resp, err := g.musicService.CreatePlaylist(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) addToPlaylist(c *gin.Context) {
	req := &pb.AddToPlaylistRequest{
		PlaylistId: c.Param("id"),
		TrackId:    c.PostForm("track_id"),
	}

	resp, err := g.musicService.AddToPlaylist(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) removeFromPlaylist(c *gin.Context) {
	req := &pb.RemoveFromPlaylistRequest{
		PlaylistId: c.Param("id"),
		TrackId:    c.Param("trackId"),
	}

	resp, err := g.musicService.RemoveFromPlaylist(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) getPlaylist(c *gin.Context) {
	playlistID := c.Param("id")
	resp, err := g.musicService.GetPlaylist(c.Request.Context(), &pb.GetPlaylistRequest{PlaylistId: playlistID})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Playlist not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) getRecommendations(c *gin.Context) {
	resp, err := g.musicService.GetRecommendations(c.Request.Context(), &pb.GetRecommendationsRequest{
		UserId: c.GetString("user_id"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) createSubscription(c *gin.Context) {
	var req paymentpb.CreateSubscriptionRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = c.GetString("user_id")

	resp, err := g.paymentService.CreateSubscription(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) cancelSubscription(c *gin.Context) {
	req := &paymentpb.CancelSubscriptionRequest{
		SubscriptionId: c.Param("id"),
	}

	resp, err := g.paymentService.CancelSubscription(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) getSubscription(c *gin.Context) {
	req := &paymentpb.GetSubscriptionRequest{
		UserId: c.GetString("user_id"),
	}

	resp, err := g.paymentService.GetSubscription(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscription not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) processPayment(c *gin.Context) {
	var req paymentpb.ProcessPaymentRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = c.GetString("user_id")

	resp, err := g.paymentService.ProcessPayment(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) getPaymentHistory(c *gin.Context) {
	req := &paymentpb.GetPaymentHistoryRequest{
		UserId: c.GetString("user_id"),
	}

	resp, err := g.paymentService.GetPaymentHistory(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) applyCoupon(c *gin.Context) {
	var req paymentpb.ApplyCouponRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = c.GetString("user_id")

	resp, err := g.paymentService.ApplyCoupon(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (g *Gateway) getInvoice(c *gin.Context) {
	req := &paymentpb.GetInvoiceRequest{
		TransactionId: c.Param("transactionId"),
	}

	resp, err := g.paymentService.GetInvoice(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invoice not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}
