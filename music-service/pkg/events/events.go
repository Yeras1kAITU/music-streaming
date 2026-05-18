package events

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

type EventPublisher struct {
	nc *nats.Conn
}

func NewEventPublisher(nc *nats.Conn) *EventPublisher {
	return &EventPublisher{nc: nc}
}

type UserRegisteredEvent struct {
	Event     string `json:"event"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Timestamp int64  `json:"timestamp"`
}

func (p *EventPublisher) PublishUserRegistered(ctx context.Context, userID, email string) {
	event := UserRegisteredEvent{
		Event:     "user_registered",
		UserID:    userID,
		Email:     email,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(event)
	if err := p.nc.Publish("user.events", data); err != nil {
		log.Printf("Failed to publish user_registered event: %v", err)
	}
}

type UserDeletedEvent struct {
	Event     string `json:"event"`
	UserID    string `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
}

func (p *EventPublisher) PublishUserDeleted(ctx context.Context, userID string) {
	event := UserDeletedEvent{
		Event:     "user_deleted",
		UserID:    userID,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(event)
	if err := p.nc.Publish("user.events", data); err != nil {
		log.Printf("Failed to publish user_deleted event: %v", err)
	}
}
