package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	pb "github.com/music-streaming/proto/music"
)

type Track struct {
	ID        string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    string `gorm:"type:uuid;not null;index"`
	Title     string `gorm:"not null;index"`
	Artist    string `gorm:"not null;index"`
	Album     string `gorm:"index"`
	Duration  int32
	Genre     string    `gorm:"index"`
	URL       string    `gorm:"not null"`
	Plays     int64     `gorm:"default:0"`
	Likes     int64     `gorm:"default:0"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

type Playlist struct {
	ID          string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID      string    `gorm:"type:uuid;not null;index"`
	Name        string    `gorm:"not null"`
	Description string    `gorm:"type:text"`
	IsPublic    bool      `gorm:"default:false"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`
}

type PlaylistTrack struct {
	PlaylistID string    `gorm:"type:uuid;primaryKey"`
	TrackID    string    `gorm:"type:uuid;primaryKey"`
	AddedAt    time.Time `gorm:"autoCreateTime"`
}

type Like struct {
	UserID    string    `gorm:"type:uuid;primaryKey"`
	TrackID   string    `gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

type musicServiceServer struct {
	pb.UnimplementedMusicServiceServer
	db         *gorm.DB
	redis      *redis.Client
	nc         *nats.Conn
	uploadPath string
	jobQueue   chan Job
	wg         sync.WaitGroup
}

type Job struct {
	ID      string
	Type    string
	Payload map[string]interface{}
}

func newMusicServiceServer(db *gorm.DB, redis *redis.Client, nc *nats.Conn, uploadPath string) *musicServiceServer {
	s := &musicServiceServer{
		db:         db,
		redis:      redis,
		nc:         nc,
		uploadPath: uploadPath,
		jobQueue:   make(chan Job, 100),
	}
	for i := 0; i < 5; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}
	return s
}

func (s *musicServiceServer) worker(id int) {
	defer s.wg.Done()
	for job := range s.jobQueue {
		s.processJob(job)
	}
}

func (s *musicServiceServer) processJob(job Job) {
	idempotencyKey := "job:" + job.ID
	exists, _ := s.redis.Exists(context.Background(), idempotencyKey).Result()
	if exists > 0 {
		log.Printf("Job %s already processed", job.ID)
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.executeJob(job); err == nil {
			s.redis.Set(context.Background(), idempotencyKey, "done", 24*time.Hour)
			return
		}
		time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
	}
	log.Printf("Job %s failed after 3 retries", job.ID)
}

func (s *musicServiceServer) executeJob(job Job) error {
	switch job.Type {
	case "transcode":
		log.Printf("Transcoding: %v", job.Payload["input_path"])
		time.Sleep(2 * time.Second)
		return nil
	default:
		return nil
	}
}

func (s *musicServiceServer) UploadTrack(ctx context.Context, req *pb.UploadTrackRequest) (*pb.UploadTrackResponse, error) {
	rateKey := "rate:upload:" + req.UserId
	count, _ := s.redis.Incr(ctx, rateKey).Result()
	if count > 10 {
		return nil, status.Error(codes.ResourceExhausted, "Upload limit exceeded")
	}
	if count == 1 {
		s.redis.Expire(ctx, rateKey, time.Hour)
	}

	trackID := uuid.New().String()
	filename := fmt.Sprintf("%s.mp3", trackID)
	filePath := filepath.Join(s.uploadPath, filename)

	os.MkdirAll(s.uploadPath, 0755)
	if err := os.WriteFile(filePath, req.AudioData, 0644); err != nil {
		return nil, status.Error(codes.Internal, "Failed to save file")
	}

	track := &Track{
		ID:       trackID,
		UserID:   req.UserId,
		Title:    req.Title,
		Artist:   req.Artist,
		Album:    req.Album,
		Duration: req.Duration,
		Genre:    req.Genre,
		URL:      fmt.Sprintf("/uploads/%s", filename),
	}

	if err := s.db.Create(track).Error; err != nil {
		os.Remove(filePath)
		return nil, status.Error(codes.Internal, "Failed to save metadata")
	}

	s.jobQueue <- Job{
		ID:   trackID,
		Type: "transcode",
		Payload: map[string]interface{}{
			"input_path":  filePath,
			"output_path": filepath.Join(s.uploadPath, fmt.Sprintf("%s_320k.mp3", trackID)),
		},
	}

	event := fmt.Sprintf(`{"event":"track_uploaded","track_id":"%s","user_id":"%s"}`, trackID, req.UserId)
	s.nc.Publish("music.events", []byte(event))

	return &pb.UploadTrackResponse{TrackId: trackID, Message: "Upload successful"}, nil
}

func (s *musicServiceServer) GetTrack(ctx context.Context, req *pb.GetTrackRequest) (*pb.Track, error) {
	var track Track
	if err := s.db.First(&track, "id = ?", req.TrackId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Track not found")
	}
	s.db.Model(&track).UpdateColumn("plays", gorm.Expr("plays + ?", 1))

	return &pb.Track{
		Id:       track.ID,
		UserId:   track.UserID,
		Title:    track.Title,
		Artist:   track.Artist,
		Album:    track.Album,
		Duration: track.Duration,
		Genre:    track.Genre,
		Url:      track.URL,
		Plays:    int32(track.Plays),
		Likes:    int32(track.Likes),
	}, nil
}

func (s *musicServiceServer) ListTracks(ctx context.Context, req *pb.ListTracksRequest) (*pb.ListTracksResponse, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	var tracks []Track
	offset := (req.Page - 1) * req.PageSize
	var total int64
	s.db.Model(&Track{}).Count(&total)
	s.db.Offset(int(offset)).Limit(int(req.PageSize)).Order("created_at DESC").Find(&tracks)

	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = &pb.Track{
			Id: t.ID, Title: t.Title, Artist: t.Artist, Album: t.Album,
			Duration: t.Duration, Url: t.URL, Plays: int32(t.Plays),
		}
	}
	return &pb.ListTracksResponse{Tracks: pbTracks, Total: int32(total)}, nil
}

func (s *musicServiceServer) SearchTracks(ctx context.Context, req *pb.SearchTracksRequest) (*pb.SearchTracksResponse, error) {
	var tracks []Track
	query := "%" + strings.ToLower(req.Query) + "%"
	s.db.Where("LOWER(title) LIKE ? OR LOWER(artist) LIKE ?", query, query).Find(&tracks)

	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = &pb.Track{Id: t.ID, Title: t.Title, Artist: t.Artist}
	}
	return &pb.SearchTracksResponse{Tracks: pbTracks, Total: int32(len(tracks))}, nil
}

func (s *musicServiceServer) CreatePlaylist(ctx context.Context, req *pb.CreatePlaylistRequest) (*pb.Playlist, error) {
	playlist := &Playlist{
		ID:          uuid.New().String(),
		UserID:      req.UserId,
		Name:        req.Name,
		Description: req.Description,
		IsPublic:    req.IsPublic,
	}
	if err := s.db.Create(playlist).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to create playlist")
	}
	return &pb.Playlist{
		Id: playlist.ID, UserId: playlist.UserID, Name: playlist.Name,
		Description: playlist.Description, IsPublic: playlist.IsPublic,
	}, nil
}

func (s *musicServiceServer) AddToPlaylist(ctx context.Context, req *pb.AddToPlaylistRequest) (*pb.Playlist, error) {
	var playlist Playlist
	if err := s.db.First(&playlist, "id = ? AND user_id = ?", req.PlaylistId, req.UserId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Playlist not found")
	}
	playlistTrack := &PlaylistTrack{PlaylistID: req.PlaylistId, TrackID: req.TrackId}
	if err := s.db.Create(playlistTrack).Error; err != nil {
		return nil, status.Error(codes.Internal, "Failed to add track")
	}
	return &pb.Playlist{Id: playlist.ID, Name: playlist.Name}, nil
}

func (s *musicServiceServer) RemoveFromPlaylist(ctx context.Context, req *pb.RemoveFromPlaylistRequest) (*pb.Playlist, error) {
	s.db.Delete(&PlaylistTrack{}, "playlist_id = ? AND track_id = ?", req.PlaylistId, req.TrackId)
	return &pb.Playlist{Id: req.PlaylistId}, nil
}

func (s *musicServiceServer) GetPlaylist(ctx context.Context, req *pb.GetPlaylistRequest) (*pb.Playlist, error) {
	var playlist Playlist
	if err := s.db.First(&playlist, "id = ?", req.PlaylistId).Error; err != nil {
		return nil, status.Error(codes.NotFound, "Playlist not found")
	}
	var tracks []Track
	s.db.Table("tracks").Joins("JOIN playlist_tracks ON playlist_tracks.track_id = tracks.id").
		Where("playlist_tracks.playlist_id = ?", req.PlaylistId).Find(&tracks)
	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = &pb.Track{Id: t.ID, Title: t.Title, Artist: t.Artist}
	}
	return &pb.Playlist{
		Id: playlist.ID, Name: playlist.Name, Description: playlist.Description,
		Tracks: pbTracks, IsPublic: playlist.IsPublic, TrackCount: int32(len(tracks)),
	}, nil
}

func (s *musicServiceServer) GetUserPlaylists(ctx context.Context, req *pb.GetUserPlaylistsRequest) (*pb.GetUserPlaylistsResponse, error) {
	var playlists []Playlist
	s.db.Where("user_id = ? OR is_public = true", req.UserId).Find(&playlists)
	pbPlaylists := make([]*pb.Playlist, len(playlists))
	for i, p := range playlists {
		var count int64
		s.db.Model(&PlaylistTrack{}).Where("playlist_id = ?", p.ID).Count(&count)
		pbPlaylists[i] = &pb.Playlist{Id: p.ID, Name: p.Name, TrackCount: int32(count)}
	}
	return &pb.GetUserPlaylistsResponse{Playlists: pbPlaylists}, nil
}

func (s *musicServiceServer) StreamTrack(req *pb.StreamTrackRequest, stream pb.MusicService_StreamTrackServer) error {
	var track Track
	if err := s.db.First(&track, "id = ?", req.TrackId).Error; err != nil {
		return status.Error(codes.NotFound, "Track not found")
	}
	file, err := os.Open(track.URL)
	if err != nil {
		return status.Error(codes.Internal, "Failed to open file")
	}
	defer file.Close()

	buffer := make([]byte, 64*1024)
	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Error(codes.Internal, "Read error")
		}
		if err := stream.Send(&pb.TrackChunk{Data: buffer[:n]}); err != nil {
			return err
		}
	}
	return nil
}

func (s *musicServiceServer) GetRecommendations(ctx context.Context, req *pb.GetRecommendationsRequest) (*pb.RecommendationsResponse, error) {
	var tracks []Track
	s.db.Order("plays DESC").Limit(int(req.Limit)).Find(&tracks)
	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = &pb.Track{Id: t.ID, Title: t.Title, Artist: t.Artist}
	}
	return &pb.RecommendationsResponse{Tracks: pbTracks}, nil
}

func (s *musicServiceServer) LikeTrack(ctx context.Context, req *pb.LikeTrackRequest) (*pb.LikeTrackResponse, error) {
	var like Like
	err := s.db.Where("user_id = ? AND track_id = ?", req.UserId, req.TrackId).First(&like).Error
	if err == nil {
		s.db.Delete(&like)
		s.db.Model(&Track{}).Where("id = ?", req.TrackId).UpdateColumn("likes", gorm.Expr("likes - ?", 1))
		return &pb.LikeTrackResponse{Liked: false}, nil
	}
	s.db.Create(&Like{UserID: req.UserId, TrackID: req.TrackId})
	s.db.Model(&Track{}).Where("id = ?", req.TrackId).UpdateColumn("likes", gorm.Expr("likes + ?", 1))
	return &pb.LikeTrackResponse{Liked: true}, nil
}

func (s *musicServiceServer) GetLikedTracks(ctx context.Context, req *pb.GetLikedTracksRequest) (*pb.GetLikedTracksResponse, error) {
	var tracks []Track
	s.db.Table("tracks").Joins("JOIN likes ON likes.track_id = tracks.id").
		Where("likes.user_id = ?", req.UserId).Find(&tracks)
	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = &pb.Track{Id: t.ID, Title: t.Title, Artist: t.Artist}
	}
	return &pb.GetLikedTracksResponse{Tracks: pbTracks, Total: int32(len(tracks))}, nil
}

func (s *musicServiceServer) AddToQueue(ctx context.Context, req *pb.AddToQueueRequest) (*pb.AddToQueueResponse, error) {
	key := "queue:" + req.UserId
	size, _ := s.redis.LLen(ctx, key).Result()
	data := fmt.Sprintf(`{"track_id":"%s","added_at":%d}`, req.TrackId, time.Now().Unix())
	s.redis.RPush(ctx, key, data)
	s.redis.Expire(ctx, key, 24*time.Hour)
	return &pb.AddToQueueResponse{Position: int32(size) + 1}, nil
}

func (s *musicServiceServer) GetQueue(ctx context.Context, req *pb.GetQueueRequest) (*pb.GetQueueResponse, error) {
	items, _ := s.redis.LRange(ctx, "queue:"+req.UserId, 0, -1).Result()
	return &pb.GetQueueResponse{Tracks: []*pb.Track{}}, nil
}

func main() {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		getEnv("DB_HOST", "postgres"), getEnv("DB_USER", "music_user"),
		getEnv("DB_PASSWORD", "music_password"), getEnv("DB_NAME", "music_db"),
		getEnv("DB_PORT", "5432"))

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("DB connection failed: %v", err)
	}
	db.AutoMigrate(&Track{}, &Playlist{}, &PlaylistTrack{}, &Like{})

	redisClient := redis.NewClient(&redis.Options{Addr: getEnv("REDIS_ADDR", "redis:6379")})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}

	nc, err := nats.Connect(getEnv("NATS_URL", "nats://nats:4222"))
	if err != nil {
		log.Fatalf("NATS connection failed: %v", err)
	}
	defer nc.Close()

	uploadPath := getEnv("UPLOAD_PATH", "/uploads")
	os.MkdirAll(uploadPath, 0755)

	grpcServer := grpc.NewServer()
	pb.RegisterMusicServiceServer(grpcServer, newMusicServiceServer(db, redisClient, nc, uploadPath))

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("music-service", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, _ := net.Listen("tcp", ":50052")
	log.Println("Music service running on :50052")
	log.Fatal(grpcServer.Serve(lis))
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
