package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrTrackNotFound     = errors.New("track not found")
	ErrPlaylistNotFound  = errors.New("playlist not found")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrInvalidInput      = errors.New("invalid input")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

type Track struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	Artist    string    `json:"artist"`
	Album     string    `json:"album"`
	Duration  int32     `json:"duration"`
	Genre     string    `json:"genre"`
	URL       string    `json:"url"`
	Plays     int32     `json:"plays"`
	Likes     int32     `json:"likes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Playlist struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsPublic    bool      `json:"is_public"`
	Tracks      []Track   `json:"tracks,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Like struct {
	UserID    string    `json:"user_id"`
	TrackID   string    `json:"track_id"`
	CreatedAt time.Time `json:"created_at"`
}

type TrackRepository interface {
	Create(ctx context.Context, track *Track) error
	GetByID(ctx context.Context, id string) (*Track, error)
	Update(ctx context.Context, track *Track) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, page, pageSize int32) ([]Track, int64, error)
	Search(ctx context.Context, query string, page, pageSize int32) ([]Track, int64, error)
	IncrementPlays(ctx context.Context, id string) error
	GetByUserID(ctx context.Context, userID string) ([]Track, error)
}

type PlaylistRepository interface {
	Create(ctx context.Context, playlist *Playlist) error
	GetByID(ctx context.Context, id string) (*Playlist, error)
	Update(ctx context.Context, playlist *Playlist) error
	Delete(ctx context.Context, id string) error
	GetByUserID(ctx context.Context, userID string) ([]Playlist, error)
	AddTrack(ctx context.Context, playlistID, trackID string) error
	RemoveTrack(ctx context.Context, playlistID, trackID string) error
	GetTracks(ctx context.Context, playlistID string) ([]Track, error)
}

type LikeRepository interface {
	Create(ctx context.Context, userID, trackID string) error
	Delete(ctx context.Context, userID, trackID string) error
	GetUserLikes(ctx context.Context, userID string, page, pageSize int32) ([]Track, int64, error)
	IsLiked(ctx context.Context, userID, trackID string) (bool, error)
	CountByTrack(ctx context.Context, trackID string) (int32, error)
}
