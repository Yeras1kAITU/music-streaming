package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"

	"github.com/music-streaming/music-service/internal/domain"
	"github.com/music-streaming/music-service/internal/worker"
	"github.com/music-streaming/music-service/pkg/cache"
	"github.com/music-streaming/music-service/pkg/events"
)

type TrackService struct {
	repo       domain.TrackRepository
	cache      *cache.TrackCache
	events     *events.EventPublisher
	worker     *worker.WorkerPool
	uploadPath string
}

func NewTrackService(
	repo domain.TrackRepository,
	cache *cache.TrackCache,
	events *events.EventPublisher,
	worker *worker.WorkerPool,
	uploadPath string,
) *TrackService {
	return &TrackService{
		repo:       repo,
		cache:      cache,
		events:     events,
		worker:     worker,
		uploadPath: uploadPath,
	}
}

func (s *TrackService) UploadTrack(ctx context.Context, userID, title, artist, album, genre string, duration int32, audioData []byte) (*domain.Track, error) {
	// Rate limiting
	if err := s.checkRateLimit(ctx, userID); err != nil {
		return nil, err
	}

	// Generate track ID
	trackID := uuid.New().String()

	// Save audio file
	filename := fmt.Sprintf("%s.mp3", trackID)
	filePath := filepath.Join(s.uploadPath, filename)

	if err := os.WriteFile(filePath, audioData, 0644); err != nil {
		return nil, fmt.Errorf("failed to save audio file: %w", err)
	}

	// Create track record
	track := &domain.Track{
		ID:        trackID,
		UserID:    userID,
		Title:     title,
		Artist:    artist,
		Album:     album,
		Genre:     genre,
		Duration:  duration,
		URL:       fmt.Sprintf("/uploads/%s", filename),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.repo.Create(ctx, track); err != nil {
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to create track record: %w", err)
	}

	// Queue transcoding job
	s.worker.Enqueue(worker.Job{
		ID:   trackID,
		Type: "transcode",
		Payload: map[string]interface{}{
			"input_path":  filePath,
			"output_path": filepath.Join(s.uploadPath, fmt.Sprintf("%s_transcoded.mp3", trackID)),
			"format":      "mp3",
		},
	})

	// Publish event
	s.events.PublishTrackUploaded(ctx, trackID, userID, title)

	// Cache track
	s.cache.SetTrack(ctx, trackID, track, 1*time.Hour)

	return track, nil
}

func (s *TrackService) GetTrack(ctx context.Context, trackID string) (*domain.Track, error) {
	// Check cache first
	if track, err := s.cache.GetTrack(ctx, trackID); err == nil {
		// Increment plays asynchronously
		go s.repo.IncrementPlays(context.Background(), trackID)
		return track, nil
	}

	// Get from database
	track, err := s.repo.GetByID(ctx, trackID)
	if err != nil {
		return nil, err
	}

	// Cache for future requests
	s.cache.SetTrack(ctx, trackID, track, 1*time.Hour)

	// Increment plays
	go s.repo.IncrementPlays(context.Background(), trackID)

	return track, nil
}

func (s *TrackService) ListTracks(ctx context.Context, page, pageSize int32) ([]domain.Track, int64, error) {
	return s.repo.List(ctx, page, pageSize)
}

func (s *TrackService) SearchTracks(ctx context.Context, query string, page, pageSize int32) ([]domain.Track, int64, error) {
	return s.repo.Search(ctx, query, page, pageSize)
}

func (s *TrackService) StreamTrack(ctx context.Context, trackID string, writer io.Writer) error {
	track, err := s.repo.GetByID(ctx, trackID)
	if err != nil {
		return err
	}

	file, err := os.Open(track.URL)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(writer, file)
	return err
}

func (s *TrackService) IncrementPlays(ctx context.Context, trackID string) error {
	return s.repo.IncrementPlays(ctx, trackID)
}

func (s *TrackService) checkRateLimit(ctx context.Context, userID string) error {
	// In production, implement Redis-based rate limiting
	return nil
}
