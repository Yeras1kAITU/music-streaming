package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/music-streaming/user-service/internal/domain"
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

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) domain.UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *domain.User) error {
	u := &User{
		ID:       user.ID,
		Email:    user.Email,
		Password: user.Password,
		Username: user.Username,
		Role:     user.Role,
		Verified: user.Verified,
	}
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *userRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	var user User
	err := r.db.WithContext(ctx).First(&user, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &domain.User{
		ID:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		Role:      user.Role,
		Verified:  user.Verified,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}, nil
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user User
	err := r.db.WithContext(ctx).First(&user, "email = ?", email).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &domain.User{
		ID:        user.ID,
		Email:     user.Email,
		Password:  user.Password,
		Username:  user.Username,
		Role:      user.Role,
		Verified:  user.Verified,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}, nil
}

func (r *userRepository) Update(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", user.ID).Updates(map[string]interface{}{
		"username":   user.Username,
		"email":      user.Email,
		"updated_at": time.Now(),
	}).Error
}

func (r *userRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&User{}, "id = ?", id).Error
}

func (r *userRepository) UpdatePassword(ctx context.Context, id, hashedPassword string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("password", hashedPassword).Error
}

func (r *userRepository) VerifyEmail(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Update("verified", true).Error
}
