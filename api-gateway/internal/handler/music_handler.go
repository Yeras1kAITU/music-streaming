package handler

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	pb "github.com/music-streaming/proto/music"
)

type MusicHandler struct {
	musicClient pb.MusicServiceClient
}

func NewMusicHandler(client pb.MusicServiceClient) *MusicHandler {
	return &MusicHandler{musicClient: client}
}

func (h *MusicHandler) UploadTrack(c *gin.Context) {
	userID, _ := c.Get("user_id")

	title := c.PostForm("title")
	artist := c.PostForm("artist")
	album := c.PostForm("album")
	genre := c.PostForm("genre")

	file, err := c.FormFile("audio")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Audio file required"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()

	audioData, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
		return
	}

	req := &pb.UploadTrackRequest{
		UserId:    userID.(string),
		Title:     title,
		Artist:    artist,
		Album:     album,
		Genre:     genre,
		Duration:  0,
		AudioData: audioData,
	}

	resp, err := h.musicClient.UploadTrack(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) GetTrack(c *gin.Context) {
	trackID := c.Param("id")
	resp, err := h.musicClient.GetTrack(c.Request.Context(), &pb.GetTrackRequest{TrackId: trackID})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Track not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) ListTracks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	resp, err := h.musicClient.ListTracks(c.Request.Context(), &pb.ListTracksRequest{
		Page:     int32(page),
		PageSize: int32(pageSize),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) SearchTracks(c *gin.Context) {
	query := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	resp, err := h.musicClient.SearchTracks(c.Request.Context(), &pb.SearchTracksRequest{
		Query:    query,
		Page:     int32(page),
		PageSize: int32(pageSize),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) CreatePlaylist(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsPublic    bool   `json:"is_public"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.musicClient.CreatePlaylist(c.Request.Context(), &pb.CreatePlaylistRequest{
		UserId:      userID.(string),
		Name:        req.Name,
		Description: req.Description,
		IsPublic:    req.IsPublic,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) GetUserPlaylists(c *gin.Context) {
	userID, _ := c.Get("user_id")

	resp, err := h.musicClient.GetUserPlaylists(c.Request.Context(), &pb.GetUserPlaylistsRequest{UserId: userID.(string)})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) GetPlaylist(c *gin.Context) {
	playlistID := c.Param("id")
	userID, _ := c.Get("user_id")

	resp, err := h.musicClient.GetPlaylist(c.Request.Context(), &pb.GetPlaylistRequest{
		PlaylistId: playlistID,
		UserId:     userID.(string),
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Playlist not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) AddToPlaylist(c *gin.Context) {
	playlistID := c.Param("id")
	userID, _ := c.Get("user_id")

	var req struct {
		TrackID string `json:"track_id"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.musicClient.AddToPlaylist(c.Request.Context(), &pb.AddToPlaylistRequest{
		PlaylistId: playlistID,
		TrackId:    req.TrackID,
		UserId:     userID.(string),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) RemoveFromPlaylist(c *gin.Context) {
	playlistID := c.Param("id")
	trackID := c.Param("trackId")
	userID, _ := c.Get("user_id")

	resp, err := h.musicClient.RemoveFromPlaylist(c.Request.Context(), &pb.RemoveFromPlaylistRequest{
		PlaylistId: playlistID,
		TrackId:    trackID,
		UserId:     userID.(string),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) LikeTrack(c *gin.Context) {
	trackID := c.Param("id")
	userID, _ := c.Get("user_id")

	resp, err := h.musicClient.LikeTrack(c.Request.Context(), &pb.LikeTrackRequest{
		TrackId: trackID,
		UserId:  userID.(string),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) AddToQueue(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		TrackID string `json:"track_id"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.musicClient.AddToQueue(c.Request.Context(), &pb.AddToQueueRequest{
		UserId:  userID.(string),
		TrackId: req.TrackID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) GetQueue(c *gin.Context) {
	userID, _ := c.Get("user_id")

	resp, err := h.musicClient.GetQueue(c.Request.Context(), &pb.GetQueueRequest{UserId: userID.(string)})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) GetRecommendations(c *gin.Context) {
	userID, _ := c.Get("user_id")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	resp, err := h.musicClient.GetRecommendations(c.Request.Context(), &pb.GetRecommendationsRequest{
		UserId: userID.(string),
		Limit:  int32(limit),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *MusicHandler) StreamTrack(c *gin.Context) {

	id := c.Param("id")

	req := &pb.StreamTrackRequest{
		TrackId: id,
	}

	stream, err := h.musicClient.StreamTrack(
		context.Background(),
		req,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.Header("Content-Type", "audio/mpeg")
	c.Header("Accept-Ranges", "bytes")

	for {

		chunk, err := stream.Recv()

		if err != nil {
			break
		}

		_, writeErr := c.Writer.Write(chunk.Data)

		if writeErr != nil {
			return
		}

		c.Writer.Flush()
	}
}
