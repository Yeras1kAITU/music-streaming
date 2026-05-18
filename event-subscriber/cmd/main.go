package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type EventHandler struct {
	db *gorm.DB
}

type UserRegisteredEvent struct {
	Event     string `json:"event"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	Timestamp int64  `json:"timestamp"`
}

type TrackUploadedEvent struct {
	Event     string `json:"event"`
	TrackID   string `json:"track_id"`
	UserID    string `json:"user_id"`
	Title     string `json:"title"`
	Timestamp int64  `json:"timestamp"`
}

type PaymentCompletedEvent struct {
	Event         string  `json:"event"`
	TransactionID string  `json:"transaction_id"`
	UserID        string  `json:"user_id"`
	Amount        float64 `json:"amount"`
	Timestamp     int64   `json:"timestamp"`
}

type Notification struct {
	ID        string    `gorm:"primaryKey"`
	UserID    string    `gorm:"index"`
	Type      string    `gorm:"index"`
	Message   string    `gorm:"type:text"`
	Read      bool      `gorm:"default:false"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

type UserStat struct {
	UserID         string    `gorm:"primaryKey"`
	TracksCount    int32     `gorm:"default:0"`
	PlaylistsCount int32     `gorm:"default:0"`
	TotalPlays     int32     `gorm:"default:0"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime"`
}

func main() {
	nc, err := nats.Connect(os.Getenv("NATS_URL"),
		nats.Timeout(10*time.Second),
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	db.AutoMigrate(&Notification{}, &UserStat{})

	handler := &EventHandler{db: db}

	_, err = nc.Subscribe("user.events", func(msg *nats.Msg) {
		var event UserRegisteredEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("Failed to parse user event: %v", err)
			return
		}
		handler.handleUserRegistered(event)
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to user.events: %v", err)
	}

	_, err = nc.Subscribe("music.events", func(msg *nats.Msg) {
		var event TrackUploadedEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("Failed to parse music event: %v", err)
			return
		}
		handler.handleTrackUploaded(event)
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to music.events: %v", err)
	}

	_, err = nc.Subscribe("payment.events", func(msg *nats.Msg) {
		var event PaymentCompletedEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Printf("Failed to parse payment event: %v", err)
			return
		}
		handler.handlePaymentCompleted(event)
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to payment.events: %v", err)
	}

	_, err = nc.QueueSubscribe("analytics.events", "analytics-workers", func(msg *nats.Msg) {
		log.Printf("Processing analytics: %s", string(msg.Data))
	})
	if err != nil {
		log.Printf("Failed to subscribe to analytics queue: %v", err)
	}

	log.Println("Event subscriber started. Listening for events...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down event subscriber...")
	nc.Drain()
}

func (h *EventHandler) handleUserRegistered(event UserRegisteredEvent) {
	log.Printf("Processing user registration: %s", event.UserID)

	notification := &Notification{
		ID:      uuid.New().String(),
		UserID:  event.UserID,
		Type:    "welcome",
		Message: fmt.Sprintf("Welcome to MusicStream! Start exploring music now!"),
		Read:    false,
	}

	if err := h.db.Create(notification).Error; err != nil {
		log.Printf("Failed to create notification: %v", err)
	}

	stat := &UserStat{
		UserID: event.UserID,
	}
	h.db.Create(stat)

	log.Printf("User %s registered successfully", event.UserID)
}

func (h *EventHandler) handleTrackUploaded(event TrackUploadedEvent) {
	log.Printf("Processing track upload: %s by %s", event.Title, event.UserID)

	h.db.Model(&UserStat{}).
		Where("user_id = ?", event.UserID).
		Update("tracks_count", gorm.Expr("tracks_count + ?", 1))
}

func (h *EventHandler) handlePaymentCompleted(event PaymentCompletedEvent) {
	log.Printf("Processing payment: %s - %.2f", event.TransactionID, event.Amount)

	notification := &Notification{
		ID:      uuid.New().String(),
		UserID:  event.UserID,
		Type:    "payment_receipt",
		Message: fmt.Sprintf("Payment of %.2f processed successfully.", event.Amount),
		Read:    false,
	}

	h.db.Create(notification)
}
