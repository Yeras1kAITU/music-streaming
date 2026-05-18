package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"

	userpb "github.com/music-streaming/proto/user"
)

type AuthMiddleware struct {
	userClient userpb.UserServiceClient
	redis      *redis.Client
}

func NewAuthMiddleware(client userpb.UserServiceClient, redis *redis.Client) *AuthMiddleware {
	return &AuthMiddleware{
		userClient: client,
		redis:      redis,
	}
}

func (m *AuthMiddleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing authorization header"})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization format"})
			c.Abort()
			return
		}

		token := parts[1]

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		resp, err := m.userClient.ValidateToken(ctx, &userpb.ValidateTokenRequest{Token: token})
		if err != nil || !resp.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		c.Set("user_id", resp.UserId)
		c.Next()
	}
}
