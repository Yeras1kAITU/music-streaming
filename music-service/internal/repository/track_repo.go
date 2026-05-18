package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/music-streaming/music-service/internal/domain"
)

type Track struct {
	ID        string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    string `gorm:"type:uuid;not null;index"`
	Title     string `gorm:"not null;index"`
	Artist    string `gorm:"not null;index"`
	Album     string `gorm:"index"`
	Duration  int32
	Genre     string `gorm:"index"`
	URL       string `gorm:"not null"`
	Plays     int32  `gorm:"default:0"`
	Likes     int32  `gorm:"default:0"`
	CreatedAt int64  `gorm:"autoCreateTime"`
	UpdatedAt int64  `gorm:"autoUpdateTime"`
}

func (t *Track) ToDomain() *domain.Track {
	return &domain.Track{
		ID:        t.ID,
		UserID:    t.UserID,
		Title:     t.Title,
		Artist:    t.Artist,
		Album:     t.Album,
		Duration:  t.Duration,
		Genre:     t.Genre,
		URL:       t.URL,
		Plays:     t.Plays,
		Likes:     t.Likes,
		CreatedAt: time.Unix(0, t.CreatedAt),
		UpdatedAt: time.Unix(0, t.UpdatedAt),
	}
}

type trackRepository struct {
	db *gorm.DB
}

func NewTrackRepository(db *gorm.DB) domain.TrackRepository {
	return &trackRepository{db: db}
}

func (r *trackRepository) Create(ctx context.Context, track *domain.Track) error {
	t := &Track{
		ID:       track.ID,
		UserID:   track.UserID,
		Title:    track.Title,
		Artist:   track.Artist,
		Album:    track.Album,
		Duration: track.Duration,
		Genre:    track.Genre,
		URL:      track.URL,
	}
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *trackRepository) GetByID(ctx context.Context, id string) (*domain.Track, error) {
	var track Track
	err := r.db.WithContext(ctx).First(&track, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrTrackNotFound
	}
	return track.ToDomain(), err
}

func (r *trackRepository) Update(ctx context.Context, track *domain.Track) error {
	return r.db.WithContext(ctx).Model(&Track{}).
		Where("id = ?", track.ID).
		Updates(map[string]interface{}{
			"title":      track.Title,
			"artist":     track.Artist,
			"album":      track.Album,
			"duration":   track.Duration,
			"genre":      track.Genre,
			"updated_at": time.Now().Unix(),
		}).Error
}

func (r *trackRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&Track{}, "id = ?", id).Error
}

func (r *trackRepository) List(ctx context.Context, page, pageSize int32) ([]domain.Track, int64, error) {
	var tracks []Track
	offset := (page - 1) * pageSize

	var total int64
	r.db.WithContext(ctx).Model(&Track{}).Count(&total)

	err := r.db.WithContext(ctx).
		Offset(int(offset)).
		Limit(int(pageSize)).
		Order("created_at DESC").
		Find(&tracks).Error

	if err != nil {
		return nil, 0, err
	}

	result := make([]domain.Track, len(tracks))
	for i, t := range tracks {
		result[i] = *t.ToDomain()
	}

	return result, total, nil
}

func (r *trackRepository) Search(ctx context.Context, query string, page, pageSize int32) ([]domain.Track, int64, error) {
	var tracks []Track
	offset := (page - 1) * pageSize

	searchQuery := r.db.WithContext(ctx).
		Where("title ILIKE ? OR artist ILIKE ? OR album ILIKE ?",
			"%"+query+"%", "%"+query+"%", "%"+query+"%")

	var total int64
	searchQuery.Model(&Track{}).Count(&total)

	err := searchQuery.
		Offset(int(offset)).
		Limit(int(pageSize)).
		Order("plays DESC").
		Find(&tracks).Error

	if err != nil {
		return nil, 0, err
	}

	result := make([]domain.Track, len(tracks))
	for i, t := range tracks {
		result[i] = *t.ToDomain()
	}

	return result, total, nil
}

func (r *trackRepository) IncrementPlays(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&Track{}).
		Where("id = ?", id).
		UpdateColumn("plays", gorm.Expr("plays + ?", 1)).Error
}

func (r *trackRepository) GetByUserID(ctx context.Context, userID string) ([]domain.Track, error) {
	var tracks []Track
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&tracks).Error

	if err != nil {
		return nil, err
	}

	result := make([]domain.Track, len(tracks))
	for i, t := range tracks {
		result[i] = *t.ToDomain()
	}

	return result, nil
}
