package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/music-streaming/music-service/internal/domain"
)

type UserCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewUserCache(client *redis.Client, ttl time.Duration) *UserCache {
	return &UserCache{client: client, ttl: ttl}
}

func (c *UserCache) SetUser(ctx context.Context, userID string, user *domain.User, ttl time.Duration) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, fmt.Sprintf("user:%s", userID), data, ttl).Err()
}

func (c *UserCache) GetUser(ctx context.Context, userID string) (*domain.User, error) {
	data, err := c.client.Get(ctx, fmt.Sprintf("user:%s", userID)).Bytes()
	if err != nil {
		return nil, err
	}
	var user domain.User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *UserCache) InvalidateUser(ctx context.Context, userID string) error {
	return c.client.Del(ctx, fmt.Sprintf("user:%s", userID)).Err()
}

func (c *UserCache) SetVerificationToken(ctx context.Context, userID, token string, ttl time.Duration) error {
	return c.client.Set(ctx, fmt.Sprintf("verify:%s", userID), token, ttl).Err()
}

func (c *UserCache) GetVerificationToken(ctx context.Context, userID string) (string, error) {
	return c.client.Get(ctx, fmt.Sprintf("verify:%s", userID)).Result()
}

func (c *UserCache) DeleteVerificationToken(ctx context.Context, userID string) error {
	return c.client.Del(ctx, fmt.Sprintf("verify:%s", userID)).Err()
}

func (c *UserCache) SetResetToken(ctx context.Context, userID, token string, ttl time.Duration) error {
	return c.client.Set(ctx, fmt.Sprintf("reset:%s", userID), token, ttl).Err()
}

func (c *UserCache) GetResetTokenUser(ctx context.Context, token string) (string, error) {
	// This is simplified - in production, you'd store token->userID mapping
	keys, err := c.client.Keys(ctx, "reset:*").Result()
	if err != nil {
		return "", err
	}
	for _, key := range keys {
		val, err := c.client.Get(ctx, key).Result()
		if err == nil && val == token {
			return key[6:], nil // Remove "reset:" prefix
		}
	}
	return "", redis.Nil
}

func (c *UserCache) DeleteResetToken(ctx context.Context, token string) error {
	userID, err := c.GetResetTokenUser(ctx, token)
	if err != nil {
		return err
	}
	return c.client.Del(ctx, fmt.Sprintf("reset:%s", userID)).Err()
}

func (c *UserCache) SetTokenUser(ctx context.Context, token, userID string, ttl time.Duration) error {
	return c.client.Set(ctx, fmt.Sprintf("token:%s", token), userID, ttl).Err()
}

func (c *UserCache) GetTokenUser(ctx context.Context, token string) (string, error) {
	return c.client.Get(ctx, fmt.Sprintf("token:%s", token)).Result()
}
